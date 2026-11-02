package main

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// skipDirs are never searched for repositories.
var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"venv":         true,
	"__pycache__":  true,
	"dist":         true,
	"build":        true,
	"target":       true,
}

// findRepos returns the repositories under root as slash-separated paths
// relative to root ("." for root itself). It does not descend into repos.
func findRepos(root string, maxDepth int) []string {
	var repos []string
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel != "." && (strings.HasPrefix(d.Name(), ".") || skipDirs[d.Name()]) {
			return filepath.SkipDir
		}
		if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
			repos = append(repos, filepath.ToSlash(rel))
			return filepath.SkipDir
		}
		if rel != "." && strings.Count(rel, string(filepath.Separator))+1 >= maxDepth {
			return filepath.SkipDir
		}
		return nil
	})
	sort.Strings(repos)
	return repos
}

type commit struct {
	Hash    string   `json:"h"`
	Parents []string `json:"p"`
	Refs    []string `json:"r,omitempty"`
	Author  string   `json:"a"`
	Time    int64    `json:"t"`
	Subject string   `json:"s"`
}

type graph struct {
	Head      string     `json:"head"`
	Remotes   []string   `json:"remotes"`
	Worktrees []worktree `json:"worktrees"`
	Commits   []commit   `json:"commits"`
	// Base is the branch others are compared to, and Behind the branches that
	// fell too far behind it, by name as they appear in commit refs.
	Base   string         `json:"base"`
	Behind map[string]int `json:"behind"`
}

// maxBehind is how many commits a branch may trail the default branch before
// it gets a warning.
const maxBehind = 5

// worktree is a linked worktree; the main one is the repo itself.
type worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"` // empty when detached
	Head   string `json:"head"`
}

const (
	fieldSep  = "\x1f"
	recordSep = "\x1e"
)

// readGraph lists the newest commits of every branch and tag in
// topological order (children before their parents), like gitk --all.
// Stashes and T3 Code's checkpoint refs are bookkeeping, not history, so
// they are left out, both as commits and as ref labels.
func readGraph(ctx context.Context, dir string, limit int) (*graph, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	out, err := git(ctx, dir, "log", "--decorate-refs-exclude=refs/t3/*",
		"--exclude=refs/stash", "--exclude=refs/t3/*", "--all", "--topo-order",
		"-n", strconv.Itoa(limit),
		"--format=%H"+fieldSep+"%P"+fieldSep+"%D"+fieldSep+"%an"+fieldSep+"%at"+fieldSep+"%s"+recordSep)
	if err != nil {
		// A fresh repo without commits has nothing to show.
		if _, herr := git(ctx, dir, "rev-parse", "--verify", "-q", "HEAD"); herr != nil {
			return &graph{Remotes: []string{}, Worktrees: []worktree{}, Commits: []commit{}, Behind: map[string]int{}}, nil
		}
		return nil, err
	}
	g := &graph{Remotes: []string{}, Worktrees: []worktree{}, Commits: parseLog(out), Behind: map[string]int{}}
	if base := defaultBranch(ctx, dir); base != "" {
		g.Base = strings.TrimPrefix(strings.TrimPrefix(base, "refs/remotes/"), "refs/heads/")
		// Needs git 2.41; without it there are just no warnings.
		if refs, err := git(ctx, dir, "for-each-ref", "--format=%(refname) %(ahead-behind:"+base+")", "refs/heads", "refs/remotes"); err == nil {
			g.Behind = parseBehind(refs)
		}
	}
	if remotes, err := git(ctx, dir, "remote"); err == nil {
		g.Remotes = append(g.Remotes, strings.Fields(string(remotes))...)
	}
	if wt, err := git(ctx, dir, "worktree", "list", "--porcelain"); err == nil {
		g.Worktrees = parseWorktrees(wt)
	}
	if head, err := git(ctx, dir, "symbolic-ref", "-q", "--short", "HEAD"); err == nil {
		g.Head = strings.TrimSpace(string(head))
	} else if head, err := git(ctx, dir, "rev-parse", "--short", "HEAD"); err == nil {
		g.Head = strings.TrimSpace(string(head))
	}
	return g, nil
}

func parseLog(out []byte) []commit {
	commits := []commit{}
	for _, rec := range bytes.Split(out, []byte(recordSep)) {
		rec = bytes.TrimLeft(rec, "\n")
		if len(rec) == 0 {
			continue
		}
		f := strings.SplitN(string(rec), fieldSep, 6)
		if len(f) < 6 {
			continue
		}
		t, _ := strconv.ParseInt(f[4], 10, 64)
		c := commit{
			Hash:    f[0],
			Parents: strings.Fields(f[1]),
			Author:  f[3],
			Time:    t,
			Subject: f[5],
		}
		if c.Parents == nil {
			c.Parents = []string{}
		}
		if f[2] != "" {
			c.Refs = strings.Split(f[2], ", ")
		}
		commits = append(commits, c)
	}
	return commits
}

// defaultBranch finds the full ref of the branch to compare others to. The
// remote's goes first: it's what a rebase should target, and local main is
// often stale when the work happens in worktrees.
func defaultBranch(ctx context.Context, dir string) string {
	candidates := []string{"refs/remotes/origin/main", "refs/remotes/origin/master", "refs/heads/main", "refs/heads/master"}
	if out, err := git(ctx, dir, "symbolic-ref", "-q", "refs/remotes/origin/HEAD"); err == nil {
		candidates = append([]string{strings.TrimSpace(string(out))}, candidates...)
	}
	for _, ref := range candidates {
		if _, err := git(ctx, dir, "rev-parse", "--verify", "-q", ref); err == nil {
			return ref
		}
	}
	return ""
}

// parseBehind reads for-each-ref lines of "<ref> <ahead> <behind>". A branch
// with nothing of its own (ahead 0) is left out: it's merged or not started,
// so there's nothing to rebase.
func parseBehind(out []byte) map[string]int {
	behind := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		ahead, _ := strconv.Atoi(f[1])
		n, _ := strconv.Atoi(f[2])
		if ahead > 0 && n > maxBehind {
			behind[strings.TrimPrefix(strings.TrimPrefix(f[0], "refs/remotes/"), "refs/heads/")] = n
		}
	}
	return behind
}

// parseWorktrees reads `git worktree list --porcelain`, skipping the main
// worktree (listed first) and ones whose folder is gone.
func parseWorktrees(out []byte) []worktree {
	wts := []worktree{}
	for i, block := range strings.Split(strings.TrimSpace(string(out)), "\n\n") {
		if i == 0 {
			continue
		}
		var wt worktree
		prunable := false
		for _, line := range strings.Split(block, "\n") {
			key, val, _ := strings.Cut(line, " ")
			switch key {
			case "worktree":
				wt.Path = val
			case "HEAD":
				wt.Head = val
			case "branch":
				wt.Branch = strings.TrimPrefix(val, "refs/heads/")
			case "prunable":
				prunable = true
			}
		}
		if wt.Path != "" && !prunable {
			wts = append(wts, wt)
		}
	}
	return wts
}

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "--no-optional-locks"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	return cmd.Output()
}
