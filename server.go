package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"
)

type server struct {
	root     string
	depth    int
	limit    int
	interval time.Duration
	store    *store
	static   http.Handler
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/repos", s.handleRepos)
	mux.HandleFunc("POST /api/repos/toggle", s.handleToggle)
	mux.HandleFunc("GET /api/graph", s.handleGraph)
	mux.Handle("GET /", noCache(s.static))
	return mux
}

type repoInfo struct {
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

func (s *server) handleRepos(w http.ResponseWriter, r *http.Request) {
	all := findRepos(s.root, s.depth)
	enabled := s.store.enabled(s.root, all)
	repos := []repoInfo{}
	for _, p := range all {
		repos = append(repos, repoInfo{Path: p, Enabled: slices.Contains(enabled, p)})
	}
	writeJSON(w, map[string]any{
		"root":       s.root,
		"name":       filepath.Base(s.root),
		"intervalMs": s.interval.Milliseconds(),
		"repos":      repos,
	})
}

func (s *server) handleToggle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, ok := s.repoDir(req.Path); !ok {
		http.Error(w, "unknown repo", http.StatusNotFound)
		return
	}
	if err := s.store.setEnabled(s.root, req.Path, req.Enabled, findRepos(s.root, s.depth)); err != nil {
		log.Printf("saving config: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleGraph(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.repoDir(r.URL.Query().Get("repo"))
	if !ok {
		http.Error(w, "unknown repo", http.StatusNotFound)
		return
	}
	g, err := readGraph(r.Context(), dir, s.limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	body, _ := json.Marshal(g)
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

// repoDir resolves a repo path from the client to a repository inside root.
func (s *server) repoDir(rel string) (string, bool) {
	if rel == "" || (rel != "." && !filepath.IsLocal(filepath.FromSlash(rel))) {
		return "", false
	}
	dir := filepath.Join(s.root, filepath.FromSlash(rel))
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err != nil {
		return "", false
	}
	return dir, true
}

// noCache makes browsers revalidate the page, so a rebuilt binary's UI
// shows up on a plain reload.
func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(v)
}
