package main

import (
	"os"
	"path/filepath"
	"slices"
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
