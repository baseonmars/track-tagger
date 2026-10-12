package media

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInspectMetadataOmitsPayloadHashes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.flac")
	// Minimal STREAMINFO followed by a nonempty payload; metadata validation is
	// identical in both inspection modes, audio decoding is not attempted.
	data := append([]byte{'f', 'L', 'a', 'C', 128, 0, 0, 34}, make([]byte, 34)...)
	data = append(data, 0xff, 0xf8, 1, 2)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	full, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	light, err := InspectMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if light.AudioHash != "" || light.FileHash != "" {
		t.Fatal("metadata inspection must not supply integrity hashes")
	}
	if !reflect.DeepEqual(full.Metadata, light.Metadata) || !reflect.DeepEqual(full.Tags, light.Tags) || full.HasArtwork != light.HasArtwork {
		t.Fatal("metadata differs")
	}
	if err := os.WriteFile(path, data[:42], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = InspectMetadata(path); err != ErrWaiting {
		t.Fatalf("missing audio: %v", err)
	}
}
