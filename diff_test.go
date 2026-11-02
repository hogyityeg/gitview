package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseDiff(t *testing.T) {
	patch := "\n\ndiff --git a/x.go b/x.go\n" +
		"index 111..222 100644\n--- a/x.go\n+++ b/x.go\n" +
		"@@ -3,3 +3,3 @@ func f() {\n ctx\n--- removed, looks like a header\n++++ added, looks like a header\n\n" +
		"\\ No newline at end of file\n" +
		"diff --git a/same name b/same name\nnew file mode 100644\nindex 000..111\n--- /dev/null\n+++ b/same name\n@@ -0,0 +1 @@\n+hi\n" +
		"diff --git a/gone b/gone\ndeleted file mode 100644\nindex 111..000\nBinary files a/gone and /dev/null differ\n" +
		"diff --git a/old b/new\nsimilarity index 100%\nrename from old\nrename to new\n" +
		"diff --git \"a/t\\303\\266\\tb\" \"b/t\\303\\266\\tb\"\nold mode 100644\nnew mode 100755\n"
	got := parseDiff(patch)
	if len(got) != 5 {
		t.Fatalf("got %d files: %+v", len(got), got)
	}

	want := []diffLine{
		{Kind: "hunk", Text: "@@ -3,3 +3,3 @@ func f() {"},
		{Kind: "ctx", Old: 3, New: 3, Text: "ctx"},
		{Kind: "del", Old: 4, Text: "-- removed, looks like a header"},
		{Kind: "add", New: 4, Text: "+++ added, looks like a header"},
		{Kind: "ctx", Old: 5, New: 5, Text: ""},
		{Kind: "note", Text: "No newline at end of file"},
	}
	if f := got[0]; f.Path != "x.go" || f.Status != "modified" || f.Added != 1 || f.Deleted != 1 || !slices.Equal(f.Lines, want) {
		t.Errorf("modified file = %+v", f)
	}
	if f := got[1]; f.Path != "same name" || f.Status != "added" || f.Added != 1 {
		t.Errorf("added file = %+v", f)
	}
	if f := got[2]; f.Path != "gone" || f.Status != "deleted" || !f.Binary {
		t.Errorf("deleted binary file = %+v", f)
	}
	if f := got[3]; f.Path != "new" || f.OldPath != "old" || f.Status != "renamed" || len(f.Lines) != 0 {
		t.Errorf("renamed file = %+v", f)
	}
	if f := got[4]; f.Path != "tö\tb" || f.Status != "modified" {
		t.Errorf("quoted file = %+v", f)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadCommitDiff(t *testing.T) {
	dir := t.TempDir()
	run := gitIn(t, dir)
	run("init", "-q", "-b", "main")
	// None of this may change what gitview gets to parse.
	for _, kv := range [][2]string{
		{"diff.noprefix", "true"}, {"color.ui", "always"}, {"log.showSignature", "true"}, {"diff.mnemonicPrefix", "true"},
	} {
		run("config", kv[0], kv[1])
	}
	writeFile(t, dir, "a.txt", "one\ntwo\n")
	run("add", ".")
	run("commit", "-q", "-m", "first\n\nthe body\nsecond line")
	first := run("rev-parse", "HEAD")

	ctx := context.Background()
	d, err := readCommitDiff(ctx, dir, first)
	if err != nil {
		t.Fatal(err)
	}
	if d.Subject != "first" || d.Body != "the body\nsecond line" || d.Author != "a" || d.Merge || d.Truncated {
		t.Errorf("root commit = %+v", d)
	}
	if len(d.Files) != 1 || d.Files[0].Path != "a.txt" || d.Files[0].Status != "added" || d.Files[0].Added != 2 {
		t.Errorf("root commit files = %+v", d.Files)
	}

	run("switch", "-q", "-c", "side")
	writeFile(t, dir, "b.txt", "side\n")
	run("add", ".")
	run("commit", "-q", "-m", "side")
	run("switch", "-q", "main")
	writeFile(t, dir, "a.txt", "one\nTWO\n")
	run("commit", "-qam", "edit")
	run("merge", "-q", "--no-ff", "-m", "merge side", "side")

	// The default combined diff of this clean merge would be empty.
	d, err = readCommitDiff(ctx, dir, run("rev-parse", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if !d.Merge || len(d.Files) != 1 || d.Files[0].Path != "b.txt" {
		t.Errorf("merge = %+v", d)
	}

	if _, err := readCommitDiff(ctx, dir, strings.Repeat("0", 40)); !errors.Is(err, errUnknownCommit) {
		t.Errorf("missing commit: err = %v", err)
	}
	// A blob's hash isn't a commit, though git show would happily print it.
	if _, err := readCommitDiff(ctx, dir, run("rev-parse", first+":a.txt")); !errors.Is(err, errUnknownCommit) {
		t.Errorf("blob: err = %v", err)
	}
}

func TestReadCommitDiffTruncatesHugeDiffs(t *testing.T) {
	dir := t.TempDir()
	run := gitIn(t, dir)
	run("init", "-q", "-b", "main")
	writeFile(t, dir, "big.txt", strings.Repeat("0123456789\n", (maxDiffBytes/11)+1000))
	run("add", ".")
	run("commit", "-q", "-m", "big")

	d, err := readCommitDiff(context.Background(), dir, run("rev-parse", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if !d.Truncated || len(d.Files) != 1 {
		t.Fatalf("truncated = %v, files = %d", d.Truncated, len(d.Files))
	}
	lines := d.Files[0].Lines
	if last := lines[len(lines)-1]; last.Text != "0123456789" {
		t.Errorf("last line = %+v, want a whole line", last)
	}
}

func TestHandleDiff(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run := gitIn(t, repo)
	run("init", "-q", "-b", "main")
	writeFile(t, repo, "a.txt", "one\n")
	run("add", ".")
	run("commit", "-q", "-m", "first")
	hash := run("rev-parse", "HEAD")

	h := (&server{root: root}).routes()
	get := func(repo, hash string) *httptest.ResponseRecorder {
		q := url.Values{"repo": {repo}, "hash": {hash}}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/diff?"+q.Encode(), nil))
		return w
	}

	w := get("repo", hash)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	var d commitDiff
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Hash != hash || len(d.Files) != 1 {
		t.Errorf("diff = %+v", d)
	}

	for _, tc := range []struct {
		name, repo, hash string
		want             int
	}{
		{"option injection", "repo", "--output=/tmp/x", http.StatusBadRequest},
		{"revision syntax", "repo", "HEAD", http.StatusBadRequest},
		{"short hash", "repo", hash[:7], http.StatusBadRequest},
		{"unknown commit", "repo", strings.Repeat("0", 40), http.StatusNotFound},
		{"outside root", "../x", hash, http.StatusNotFound},
	} {
		if w := get(tc.repo, tc.hash); w.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.name, w.Code, tc.want)
		}
	}
}
