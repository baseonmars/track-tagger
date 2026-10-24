package launcher

import (
	"os"
	"path/filepath"
	"testing"

	"track-tagger/internal/app"
)

func TestPreferences(t *testing.T) {
	d := t.TempDir()
	s, e := Load(d)
	if e != nil || s.Mode != app.DryRun {
		t.Fatalf("defaults: %+v %v", s, e)
	}
	if !s.AutoCheckUpdates {
		t.Fatal("startup checks should default on")
	}
	s.Source = `\\server\music`
	if !s.UsePolling() {
		t.Fatal("UNC should poll")
	}
	s.PollOverride = true
	if s.UsePolling() {
		t.Fatal("override ignored")
	}
	s.Mode = app.Copy
	if s.Validate() == nil {
		t.Fatal("missing copy destination accepted")
	}
	s.Destination = "copies"
	if e = Save(d, s); e != nil {
		t.Fatal(e)
	}
	s.Mode = app.Write
	s.AutoCheckUpdates = false
	if e = Save(d, s); e != nil {
		t.Fatal(e)
	}
	got, e := Load(d)
	if e != nil || got.Mode != app.Write {
		t.Fatalf("roundtrip: %+v %v", got, e)
	}
	if got.AutoCheckUpdates {
		t.Fatal("disabled startup checks were not saved")
	}
	if e := os.WriteFile(filepath.Join(d, "launcher.json"), []byte(`{"mode":"dry-run"}`), 0600); e != nil {
		t.Fatal(e)
	}
	got, e = Load(d)
	if e != nil || !got.AutoCheckUpdates {
		t.Fatalf("old settings should default startup checks on: %+v %v", got, e)
	}
}
