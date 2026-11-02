package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// maxDiffBytes caps what a single commit's diff sends to the browser.
const maxDiffBytes = 2 << 20

var (
	commitHash = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

	errUnknownCommit = errors.New("unknown commit")
)

type commitDiff struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Time    int64  `json:"time"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	// Merge means the diff is against the first parent.
	Merge     bool       `json:"merge"`
	Files     []fileDiff `json:"files"`
	Truncated bool       `json:"truncated"`
}

type fileDiff struct {
	Path    string     `json:"path"`
	OldPath string     `json:"oldPath,omitempty"` // renames and copies
	Status  string     `json:"status"`
	Binary  bool       `json:"binary"`
	Added   int        `json:"added"`
	Deleted int        `json:"deleted"`
	Lines   []diffLine `json:"lines"`
}

// diffLine is one row of a unified diff: Kind is "hunk", "add", "del", "ctx"
// or "note" ("No newline at end of file"), Text is without the +/- marker.
type diffLine struct {
	Kind string `json:"k"`
	Old  int    `json:"o,omitempty"`
	New  int    `json:"n,omitempty"`
	Text string `json:"s"`
}

// readCommitDiff returns a commit and its changes. A merge is compared to its
// first parent, because the default combined diff hides what the merge brought
// in. The options pin the output format against the user's git config.
func readCommitDiff(ctx context.Context, dir, hash string) (*commitDiff, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if _, err := git(ctx, dir, "cat-file", "-e", hash+"^{commit}"); err != nil {
		return nil, errUnknownCommit
	}
	out, truncated, err := gitLimited(ctx, dir, maxDiffBytes, "-c", "core.quotePath=false", "show",
		"--patch", "--diff-merges=first-parent", "--no-color", "--no-ext-diff", "--no-textconv",
		"--no-show-signature", "--submodule=short", "--src-prefix=a/", "--dst-prefix=b/",
		"--format=%an"+fieldSep+"%at"+fieldSep+"%P"+fieldSep+"%s"+fieldSep+"%b"+recordSep, hash)
	if err != nil {
		return nil, err
	}
	head, patch, found := bytes.Cut(out, []byte(recordSep))
	f := strings.SplitN(string(head), fieldSep, 5)
	if !found || len(f) < 5 {
		return nil, errors.New("unexpected git show output")
	}
	t, _ := strconv.ParseInt(f[1], 10, 64)
	return &commitDiff{
		Hash:      hash,
		Author:    f[0],
		Time:      t,
		Subject:   f[3],
		Body:      strings.TrimSpace(f[4]),
		Merge:     len(strings.Fields(f[2])) > 1,
		Files:     parseDiff(string(patch)),
		Truncated: truncated,
	}, nil
}

// gitLimited runs git and keeps at most limit bytes of its output, cut at a
// line end, reporting whether there was more.
func gitLimited(ctx context.Context, dir string, limit int, args ...string) ([]byte, bool, error) {
	cmd := gitCmd(ctx, dir, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}
	out, err := io.ReadAll(io.LimitReader(stdout, int64(limit)+1))
	if err != nil || len(out) > limit {
		cmd.Process.Kill()
		cmd.Wait()
		if err != nil {
			return nil, false, err
		}
		return out[:bytes.LastIndexByte(out[:limit], '\n')+1], true, nil
	}
	return out, false, cmd.Wait()
}

// parseDiff reads the unified diff of git show. Once inside a hunk every line
// starts with a marker, so nothing in a file's content can look like a header.
func parseDiff(patch string) []fileDiff {
	files := []fileDiff{}
	var f *fileDiff
	inHunk := false
	oldLine, newLine := 0, 0
	for line := range strings.SplitSeq(strings.TrimRight(patch, "\n"), "\n") {
		if rest, ok := strings.CutPrefix(line, "diff --git "); ok {
			files = append(files, fileDiff{Path: headerPath(rest), Status: "modified", Lines: []diffLine{}})
			f = &files[len(files)-1]
			inHunk = false
			continue
		}
		if f == nil {
			continue
		}
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			oldLine, _ = strconv.Atoi(m[1])
			newLine, _ = strconv.Atoi(m[2])
			f.Lines = append(f.Lines, diffLine{Kind: "hunk", Text: line})
			inHunk = true
			continue
		}
		if !inHunk {
			f.readHeader(line)
			continue
		}
		switch {
		case strings.HasPrefix(line, "+"):
			f.Added++
			f.Lines = append(f.Lines, diffLine{Kind: "add", New: newLine, Text: line[1:]})
			newLine++
		case strings.HasPrefix(line, "-"):
			f.Deleted++
			f.Lines = append(f.Lines, diffLine{Kind: "del", Old: oldLine, Text: line[1:]})
			oldLine++
		case strings.HasPrefix(line, `\ `):
			f.Lines = append(f.Lines, diffLine{Kind: "note", Text: line[2:]})
		default: // with diff.suppressBlankEmpty a blank context line has no marker
			f.Lines = append(f.Lines, diffLine{Kind: "ctx", Old: oldLine, New: newLine, Text: strings.TrimPrefix(line, " ")})
			oldLine++
			newLine++
		}
	}
	return files
}

// readHeader takes in an extended header line, between "diff --git" and the
// first hunk.
func (f *fileDiff) readHeader(line string) {
	switch {
	case strings.HasPrefix(line, "new file mode "):
		f.Status = "added"
	case strings.HasPrefix(line, "deleted file mode "):
		f.Status = "deleted"
	case strings.HasPrefix(line, "Binary files "):
		f.Binary = true
	case strings.HasPrefix(line, "rename from "):
		f.Status, f.OldPath = "renamed", unquotePath(strings.TrimPrefix(line, "rename from "))
	case strings.HasPrefix(line, "rename to "):
		f.Path = unquotePath(strings.TrimPrefix(line, "rename to "))
	case strings.HasPrefix(line, "copy from "):
		f.Status, f.OldPath = "copied", unquotePath(strings.TrimPrefix(line, "copy from "))
	case strings.HasPrefix(line, "copy to "):
		f.Path = unquotePath(strings.TrimPrefix(line, "copy to "))
	}
}

// headerPath gets the path from `a/x b/x`. Both halves are the same length
// except for renames and copies, whose own header lines replace this.
func headerPath(rest string) string {
	return strings.TrimPrefix(unquotePath(rest[min(len(rest), len(rest)/2+1):]), "b/")
}

// unquotePath undoes git's C-style quoting of unusual file names.
func unquotePath(p string) string {
	if u, err := strconv.Unquote(p); err == nil {
		return u
	}
	return p
}
