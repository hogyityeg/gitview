package main

import (
	"context"
	"testing"
	"time"
)

func TestFetchRepoUpdatesRemoteBranches(t *testing.T) {
	remote, clone := t.TempDir(), t.TempDir()
	runRemote := gitIn(t, remote)
	runRemote("init", "-q", "-b", "main")
	runRemote("commit", "-q", "--allow-empty", "-m", "first")
	gitIn(t, clone)("clone", "-q", remote, ".")
	runRemote("commit", "-q", "--allow-empty", "-m", "second")
	head := runRemote("rev-parse", "HEAD")

	if err := fetchRepo(context.Background(), clone); err != nil {
		t.Fatal(err)
	}
	if got := gitIn(t, clone)("rev-parse", "origin/main"); got != head {
		t.Errorf("origin/main = %s, want %s", got, head)
	}
}

func TestFetchRepoReportsFailure(t *testing.T) {
	dir := t.TempDir()
	run := gitIn(t, dir)
	run("init", "-q")
	run("remote", "add", "origin", dir+"/missing")
	if err := fetchRepo(context.Background(), dir); err == nil {
		t.Error("fetching a missing remote succeeded")
	}
}

func TestFetcherReusesRecentFetch(t *testing.T) {
	remote, clone := t.TempDir(), t.TempDir()
	runRemote := gitIn(t, remote)
	runRemote("init", "-q", "-b", "main")
	runRemote("commit", "-q", "--allow-empty", "-m", "first")
	gitIn(t, clone)("clone", "-q", remote, ".")

	f := newFetcher(time.Hour)
	first, msg := f.fetch("clone", clone)
	if first.IsZero() || msg != "" {
		t.Fatalf("fetch = %v, %q; want a time and no error", first, msg)
	}
	runRemote("commit", "-q", "--allow-empty", "-m", "second")
	if again, _ := f.fetch("clone", clone); !again.Equal(first) {
		t.Errorf("second fetch at %v, want the first one (%v) reused", again, first)
	}
	if got, old := gitIn(t, clone)("rev-parse", "origin/main"), runRemote("rev-parse", "HEAD~1"); got != old {
		t.Errorf("origin/main = %s, want it unchanged at %s", got, old)
	}

	f.reuse = 0
	if later, _ := f.fetch("clone", clone); !later.After(first) {
		t.Errorf("fetch after the reuse window at %v, want later than %v", later, first)
	}
}

func TestFetcherKeepsLastSuccessOnFailure(t *testing.T) {
	dir := t.TempDir()
	run := gitIn(t, dir)
	run("init", "-q")
	run("remote", "add", "origin", dir+"/missing")
	at, msg := newFetcher(0).fetch("repo", dir)
	if !at.IsZero() || msg == "" {
		t.Errorf("fetch = %v, %q; want no time and an error", at, msg)
	}
}
