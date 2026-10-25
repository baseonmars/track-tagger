package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"track-tagger/internal/app"
)

// Settings contains preferences only. A saved preference never starts a service.
type Settings struct {
	Source           string   `json:"source"`
	Destination      string   `json:"destination"`
	Mode             app.Mode `json:"mode"`
	Poll             bool     `json:"poll"`
	PollOverride     bool     `json:"pollOverride"`
	CloseExplained   bool     `json:"closeExplained"`
	AutoCheckUpdates bool     `json:"autoCheckUpdates"`
}

func ConfigDir() (string, error) {
	d, e := os.UserConfigDir()
	return filepath.Join(d, "track-tagger"), e
}

// defaultSettings returns the safe fallback: preview mode with the update
// check enabled but not started.
func defaultSettings() Settings { return Settings{Mode: app.DryRun, AutoCheckUpdates: true} }

func Load(dir string) (Settings, error) {
	s := defaultSettings()
	b, e := os.ReadFile(filepath.Join(dir, "launcher.json"))
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	if e = json.Unmarshal(b, &s); e != nil {
		return defaultSettings(), fmt.Errorf("read launcher settings: %w", e)
	}
	if !validMode(s.Mode) {
		s.Mode = app.DryRun
	}
	return s, nil
}
func Save(dir string, s Settings) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, "launcher-*.tmp")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return replaceSettings(name, filepath.Join(dir, "launcher.json"))
}

// validMode reports whether m is one of the processing modes a user can
// select. Anything else is treated as unsafe and falls back to preview.
func validMode(m app.Mode) bool { return m == app.DryRun || m == app.Copy || m == app.Write }

func (s Settings) UsePolling() bool {
	if s.PollOverride {
		return s.Poll
	}
	return strings.HasPrefix(s.Source, `\\`)
}
func (s Settings) Validate() error {
	if strings.TrimSpace(s.Source) == "" {
		return fmt.Errorf("choose a music folder")
	}
	if !validMode(s.Mode) {
		return fmt.Errorf("choose a processing mode")
	}
	if s.Mode == app.Copy && strings.TrimSpace(s.Destination) == "" {
		return fmt.Errorf("choose a destination folder for copies")
	}
	return nil
}
