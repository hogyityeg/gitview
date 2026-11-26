package main

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"
)

const (
	// fetchParallel is how many repositories are fetched at once.
	fetchParallel = 4
	// fetchTimeout bounds one fetch, so a hanging remote can't stall the
	// refresh for long.
	fetchTimeout = 30 * time.Second
)

// fetcher fetches repositories' remotes on demand, right before their graph
// is read, so a refresh and a fetch are the same moment. Requests for a repo
// that was just fetched (e.g. from a second tab) reuse that fetch.
type fetcher struct {
	reuse time.Duration // a fetch younger than this is reused
	sem   chan struct{}

	mu    sync.Mutex
	repos map[string]*fetchState
}

type fetchState struct {
	mu      sync.Mutex // held while fetching
	ok      time.Time  // last successful fetch
	tried   time.Time  // last attempt
	lastErr string     // the last attempt's error, "" if it succeeded
}

func newFetcher(reuse time.Duration) *fetcher {
	return &fetcher{reuse: reuse, sem: make(chan struct{}, fetchParallel), repos: map[string]*fetchState{}}
}

// fetch fetches the repository at dir (rel in logs) unless it was just
// tried, and returns when it was last fetched successfully (zero if never)
// and the last attempt's error. A failure is logged once until it changes or clears.
func (f *fetcher) fetch(rel, dir string) (time.Time, string) {
	f.mu.Lock()
	st := f.repos[dir]
	if st == nil {
		st = &fetchState{}
		f.repos[dir] = st
	}
	f.mu.Unlock()

	st.mu.Lock()
	defer st.mu.Unlock()
	if time.Since(st.tried) < f.reuse {
		return st.ok, st.lastErr
	}
	f.sem <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	err := fetchRepo(ctx, dir)
	cancel()
	<-f.sem

	st.tried = time.Now()
	msg := ""
	if err != nil {
		msg = err.Error()
	} else {
		st.ok = st.tried
	}
	if msg != st.lastErr {
		if msg != "" {
			log.Printf("fetching %s: %s", rel, msg)
		} else {
			log.Printf("fetching %s: ok again", rel)
		}
		st.lastErr = msg
	}
	return st.ok, st.lastErr
}

// fetchRepo fetches every remote of the repository at dir, without ever
// waiting for a password or host key prompt.
func fetchRepo(ctx context.Context, dir string) error {
	cmd := gitCmd(ctx, dir, "fetch", "--all", "--prune", "--quiet", "--no-write-fetch-head")
	detach(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(strings.ReplaceAll(msg, "\n", "; "))
	}
	return nil
}
