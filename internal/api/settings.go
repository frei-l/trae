package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Prefs are the user's settings, kept as JSON next to the database.
type Prefs struct {
	Theme         string `json:"theme"`         // system, light, dark
	TextSize      int    `json:"textSize"`      // percent: 100, 110, 125, 150
	RetentionDays int    `json:"retentionDays"` // 0 keeps everything
}

func defaultPrefs() Prefs { return Prefs{Theme: "system", TextSize: 100, RetentionDays: 7} }

func (p *Prefs) clean() {
	switch p.Theme {
	case "system", "light", "dark":
	default:
		p.Theme = "system"
	}
	switch p.TextSize {
	case 100, 110, 125, 150:
	default:
		p.TextSize = 100
	}
	switch p.RetentionDays {
	case 0, 1, 7, 30:
	default:
		p.RetentionDays = 7
	}
}

// Settings loads and saves Prefs.
type Settings struct {
	path string
	mu   sync.Mutex
	p    Prefs
}

// LoadSettings reads path; a missing file gives the defaults. An empty
// path keeps settings in memory only.
func LoadSettings(path string) (*Settings, error) {
	s := &Settings{path: path, p: defaultPrefs()}
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.p); err != nil {
		s.p = defaultPrefs()
	}
	s.p.clean()
	return s, nil
}

// Get returns the current prefs.
func (s *Settings) Get() Prefs {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.p
}

// Set replaces the prefs and saves them.
func (s *Settings) Set(p Prefs) (Prefs, error) {
	p.clean()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.p = p
	if s.path == "" {
		return p, nil
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return p, err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return p, err
	}
	return p, os.Rename(tmp, s.path)
}
