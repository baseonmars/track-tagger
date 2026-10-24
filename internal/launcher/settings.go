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
func Load(dir string) (Settings, error) {
	s := Settings{Mode: app.DryRun, AutoCheckUpdates: true}
	b, e := os.ReadFile(filepath.Join(dir, "launcher.json"))
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	if e = json.Unmarshal(b, &s); e != nil {
		return Settings{Mode: app.DryRun, AutoCheckUpdates: true}, fmt.Errorf("read launcher settings: %w", e)
	}
	if s.Mode != app.DryRun && s.Mode != app.Copy && s.Mode != app.Write {
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
	if s.Mode != app.DryRun && s.Mode != app.Copy && s.Mode != app.Write {
		return fmt.Errorf("choose a processing mode")
	}
	if s.Mode == app.Copy && strings.TrimSpace(s.Destination) == "" {
		return fmt.Errorf("choose a destination folder for copies")
	}
	return nil
}
