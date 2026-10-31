package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// store persists which repos are shown, per root folder:
//
//	{"folders": {"/abs/root": {"enabled": ["a", "b/c"]}}}
type store struct {
	path string
	mu   sync.Mutex
	cfg  config
}

type config struct {
	Folders map[string]*folderConfig `json:"folders"`
}

type folderConfig struct {
	Enabled []string `json:"enabled"`
}

func defaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gitview", "config.json"), nil
}

func loadStore(path string) (*store, error) {
	s := &store{path: path}
	if err := s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// reload reads the config from disk; callers hold s.mu (or own s).
// Several gitview instances share the file, one per folder, so every write
// starts from what is on disk rather than this instance's copy.
func (s *store) reload() error {
	s.cfg = config{Folders: map[string]*folderConfig{}}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &s.cfg); err != nil {
		return err
	}
	if s.cfg.Folders == nil {
		s.cfg.Folders = map[string]*folderConfig{}
	}
	return nil
}

// enabled returns the repos shown for root. A folder without saved settings
// shows all of them: usually it is a single repo.
func (s *store) enabled(root string, all []string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.cfg.Folders[root]; f != nil {
		return slices.Clone(f.Enabled)
	}
	return slices.Clone(all)
}

// setEnabled shows or hides one repo of root. The first toggle in a folder
// starts from all repos shown, matching what enabled returned before it.
func (s *store) setEnabled(root, repo string, on bool, all []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reload(); err != nil {
		return err
	}
	f := s.cfg.Folders[root]
	if f == nil {
		f = &folderConfig{Enabled: slices.Sorted(slices.Values(all))}
		s.cfg.Folders[root] = f
	}
	i := slices.Index(f.Enabled, repo)
	switch {
	case on && i < 0:
		f.Enabled = append(f.Enabled, repo)
		slices.Sort(f.Enabled)
	case !on && i >= 0:
		f.Enabled = slices.Delete(f.Enabled, i, i+1)
	}
	return s.save()
}

// save writes the config atomically; callers hold s.mu.
func (s *store) save() error {
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", s.path, os.Getpid())
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
