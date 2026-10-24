package launcher

import (
	"testing"

	"track-tagger/internal/app"
)

func TestPreferences(t *testing.T) {
	d := t.TempDir()
	s, e := Load(d)
	if e != nil || s.Mode != app.DryRun {
		t.Fatalf("defaults: %+v %v", s, e)
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
	if e = Save(d, s); e != nil {
		t.Fatal(e)
	}
	got, e := Load(d)
	if e != nil || got.Mode != app.Write {
		t.Fatalf("roundtrip: %+v %v", got, e)
	}
}
