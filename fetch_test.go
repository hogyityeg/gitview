package main

import (
	"context"
	"testing"
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
