//go:build parser_experiment

package main

import "testing"

func TestRejectUnsafeAndDuplicatePaths(t *testing.T) {
	for _, name := range []string{"../outside.flac", "/outside.flac", "..\\outside.flac", "C:\\outside.flac"} {
		if _, err := run(input{ID: "unsafe", Files: []string{name}}); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	if _, err := run(input{ID: "duplicate", Notes: []note{{Name: "same.txt"}, {Name: "same.txt"}}, Files: []string{"a.flac"}}); err == nil {
		t.Error("accepted duplicate notes")
	}
	if _, err := run(input{ID: "collision", Notes: []note{{Name: "a.flac"}}, Files: []string{"a.flac"}}); err == nil {
		t.Error("accepted note/file collision")
	}
}
func TestUnmatchedRawTitle(t *testing.T) {
	result, err := run(input{ID: "raw", Notes: []note{{Name: "show.txt", Text: "The Artist\n2003-05-17\nSetlist:\n1. One\n2. Two\n"}}, Files: []string{"d1t01.flac"}})
	if err != nil {
		t.Fatal(err)
	}
	titles := result["titles"].(map[string]string)
	if titles["1:2"] != "Two" {
		t.Fatalf("unmatched raw title missing: %#v", titles)
	}
}
