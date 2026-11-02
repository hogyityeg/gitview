package main

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestParseLog(t *testing.T) {
	out := []byte("aaa\x1fbbb ccc\x1fHEAD -> main, origin/main, tag: v1\x1fAda\x1f1700000000\x1fMerge x\x1e\n" +
		"bbb\x1f\x1f\x1fBob\x1f1690000000\x1ffirst\x1e\n")
	got := parseLog(out)
	if len(got) != 2 {
		t.Fatalf("got %d commits, want 2", len(got))
	}
	if !slices.Equal(got[0].Parents, []string{"bbb", "ccc"}) {
		t.Errorf("parents = %v", got[0].Parents)
	}
	if !slices.Equal(got[0].Refs, []string{"HEAD -> main", "origin/main", "tag: v1"}) {
		t.Errorf("refs = %v", got[0].Refs)
	}
	if got[1].Refs != nil || len(got[1].Parents) != 0 || got[1].Subject != "first" || got[1].Time != 1690000000 {
		t.Errorf("root commit = %+v", got[1])
	}
}

// gitIn returns a function that runs git in dir.
func gitIn(t *testing.T, dir string) func(args ...string) string {
	return func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.com",
			"GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
}

func TestReadGraphFlagsBranchesFarBehindMain(t *testing.T) {
	dir := t.TempDir()
	run := gitIn(t, dir)
	made := 0
	commit := func(n int) {
		t.Helper()
		for range n {
			made++ // same-second commits on a shared parent must still differ
			run("commit", "-q", "--allow-empty", "-m", "c"+strconv.Itoa(made))
		}
	}
	branchHere := func(name string) {
		t.Helper()
		run("switch", "-q", "-c", name)
		commit(1)
		run("switch", "-q", "main")
	}
	run("init", "-q", "-b", "main")
	commit(1)
	run("branch", "merged") // nothing of its own: no warning however old
	branchHere("stale")
	branchHere("feat/old-push")
	run("update-ref", "refs/remotes/origin/feat/old-push", "feat/old-push")
	commit(1)
	branchHere("borderline") // exactly 5 behind is still fine
	commit(5)

	g, err := readGraph(context.Background(), dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"stale": 6, "feat/old-push": 6, "origin/feat/old-push": 6}
	if g.Base != "main" || !maps.Equal(g.Behind, want) {
		t.Errorf("base = %q, behind = %v, want main, %v", g.Base, g.Behind, want)
	}

	// The remote's default branch wins over the local one.
	commit(2)
	run("update-ref", "refs/remotes/origin/main", "main~2")
	if g, err = readGraph(context.Background(), dir, 100); err != nil {
		t.Fatal(err)
	}
	if g.Base != "origin/main" || !maps.Equal(g.Behind, want) {
		t.Errorf("base = %q, behind = %v, want origin/main, %v", g.Base, g.Behind, want)
	}
}

func TestDefaultBranchFollowsOriginHead(t *testing.T) {
	dir := t.TempDir()
	run := gitIn(t, dir)
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "c")
	if got := defaultBranch(context.Background(), dir); got != "refs/heads/main" {
		t.Errorf("no remote: got %q", got)
	}
	run("update-ref", "refs/remotes/origin/develop", "HEAD")
	run("update-ref", "refs/remotes/origin/main", "HEAD")
	run("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")
	if got := defaultBranch(context.Background(), dir); got != "refs/remotes/origin/develop" {
		t.Errorf("with origin/HEAD: got %q", got)
	}
}

func TestReadGraphHidesT3Refs(t *testing.T) {
	dir := t.TempDir()
	run := gitIn(t, dir)
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "real")
	head := run("rev-parse", "HEAD")
	tree := run("rev-parse", "HEAD^{tree}")
	checkpoint := run("commit-tree", tree, "-p", head, "-m", "t3 checkpoint")
	run("update-ref", "refs/t3/checkpoints/abc/ordinal/0", checkpoint)
	run("update-ref", "refs/t3/checkpoints/abc/ordinal/1", head) // on a real commit

	g, err := readGraph(context.Background(), dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Commits) != 1 || g.Commits[0].Subject != "real" {
		t.Fatalf("commits = %+v, want only the real one", g.Commits)
	}
	if !slices.Equal(g.Commits[0].Refs, []string{"HEAD -> main"}) {
		t.Errorf("refs = %v", g.Commits[0].Refs)
	}
}

func TestParseWorktrees(t *testing.T) {
	out := []byte("worktree /repo\nHEAD aaa\nbranch refs/heads/main\n\n" +
		"worktree /wt/feature\nHEAD bbb\nbranch refs/heads/feat/x\n\n" +
		"worktree /wt/detached\nHEAD ccc\ndetached\n\n" +
		"worktree /wt/gone\nHEAD ddd\nbranch refs/heads/old\nprunable gitdir file points to non-existent location\n")
	got := parseWorktrees(out)
	want := []worktree{
		{Path: "/wt/feature", Branch: "feat/x", Head: "bbb"},
		{Path: "/wt/detached", Head: "ccc"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFindRepos(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{
		"a/.git",
		"a/nested/.git", // inside a repo: not searched
		"group/b/.git",
		"node_modules/c/.git",
		".hidden/d/.git",
		"x/y/z/deep/.git", // depth 5
	} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := findRepos(root, 3)
	if want := []string{"a", "group/b"}; !slices.Equal(got, want) {
		t.Errorf("depth 3: got %v, want %v", got, want)
	}
	if got := findRepos(root, 5); !slices.Contains(got, "x/y/z/deep") {
		t.Errorf("depth 5: got %v, missing x/y/z/deep", got)
	}
}

func TestStorePersistsPerFolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg", "config.json")
	s, err := loadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.setEnabled("/one", "b", true, nil))
	must(s.setEnabled("/one", "a", true, nil))
	must(s.setEnabled("/two", "a", true, nil))
	must(s.setEnabled("/one", "b", false, nil))

	s2, err := loadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.enabled("/one", nil); !slices.Equal(got, []string{"a"}) {
		t.Errorf("/one = %v", got)
	}
	if got := s2.enabled("/two", nil); !slices.Equal(got, []string{"a"}) {
		t.Errorf("/two = %v", got)
	}
	if got := s2.enabled("/three", []string{"x"}); !slices.Equal(got, []string{"x"}) {
		t.Errorf("/three = %v, want all", got)
	}
}
