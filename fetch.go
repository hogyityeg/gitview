package main

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"
)

// fetchParallel is how many repositories are fetched at once.
const fetchParallel = 4

// fetchLoop fetches the shown repositories' remotes every interval, starting
// right away, so the graphs show what was pushed elsewhere. A failure is
// logged once until it changes or clears.
func (s *server) fetchLoop(ctx context.Context, every time.Duration) {
	lastErr := map[string]string{}
	var mu sync.Mutex
	for {
		repos := s.store.enabled(s.root, findRepos(s.root, s.depth))
		sem := make(chan struct{}, fetchParallel)
		var wg sync.WaitGroup
		for _, rel := range repos {
			dir, ok := s.repoDir(rel)
			if !ok {
				continue
			}
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				msg := ""
				if err := fetchRepo(ctx, dir); err != nil {
					msg = err.Error()
				}
				mu.Lock()
				defer mu.Unlock()
				if msg != lastErr[rel] {
					if msg != "" {
						log.Printf("fetching %s: %s", rel, msg)
					} else {
						log.Printf("fetching %s: ok again", rel)
					}
					lastErr[rel] = msg
				}
			})
		}
		wg.Wait()
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// fetchRepo fetches every remote of the repository at dir, without ever
// waiting for a password or host key prompt.
func fetchRepo(ctx context.Context, dir string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
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
