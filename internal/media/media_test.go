package media

import (
	"bytes"
	"errors"
	flac "github.com/go-flac/go-flac/v2"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func fixture(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ff := flac.File{Meta: []*flac.MetaDataBlock{{Type: flac.StreamInfo, Data: make([]byte, 34)}, {Type: flac.Application, Data: []byte("TESTunknown extension")}, {Type: flac.VorbisComment, Data: encodeComments("original vendor", []string{"ARTIST=Existing", "CUSTOM=value", "MULTI=one", "MULTI=two"})}}, Frames: bytes.NewReader([]byte{0xff, 0xf8, 0, 1, 2, 3, 4, 5})}
	if _, err = ff.WriteTo(f); err != nil {
		t.Fatal(err)
	}
}
func TestStagePreservesAudioAndRestore(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "a.flac")
	fixture(t, src)
	before, err := Inspect(src)
	if err != nil {
		t.Fatal(err)
	}
	temp := filepath.Join(d, "stage")
	after, err := Stage(src, temp, map[string]string{"ARTIST": "new", "TITLE": "Song"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if after.AudioHash != before.AudioHash || after.Tags["ARTIST"] != "Existing" || after.Tags["TITLE"] != "Song" || after.Tags["CUSTOM"] != "value" || after.Tags["MULTI"] != "one; two" {
		t.Fatalf("unexpected metadata: %+v", after)
	}
	if !bytes.Contains(after.Metadata, []byte("TESTunknown extension")) {
		t.Fatal("unknown block lost")
	}
	h, _ := Hash(src)
	if h != before.FileHash {
		t.Fatal("source changed")
	}
	restored, err := StageRestore(temp, filepath.Join(d, "restored"), before.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	if restored.FileHash != before.FileHash {
		t.Fatal("restoration not byte identical")
	}
	if _, err = Stage(src, temp, nil, "", false); err == nil {
		t.Fatal("existing staging destination overwritten")
	}
}
func TestReviewedReplacement(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "a.flac")
	fixture(t, src)
	after, err := Stage(src, filepath.Join(d, "stage"), map[string]string{"ARTIST": "new"}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if after.Tags["ARTIST"] != "new" || after.Tags["CUSTOM"] != "value" {
		t.Fatal(after.Tags)
	}
}

func TestStageInspectedChecksSourceAndReturnsStagedHash(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "a.flac")
	fixture(t, src)
	original, err := Inspect(src)
	if err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(d, "stage")
	result, err := StageInspected(src, staged, original, map[string]string{"TITLE": "Song"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := Hash(staged)
	if err != nil || result.FileHash != hash || result.AudioHash != original.AudioHash {
		t.Fatalf("staged hashes do not match: %+v, hash %q, err %v", result, hash, err)
	}
	if _, err := StageInspected(src, filepath.Join(d, "missing-hash"), Info{Metadata: original.Metadata}, nil, "", false); err == nil {
		t.Fatal("accepted metadata-only inspection")
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	nameAt := bytes.Index(data, []byte("ARTIST=Existing"))
	if nameAt < 0 {
		t.Fatal("fixture comment missing")
	}
	data[nameAt+len("ARTIST=Existin")] = 'G'
	if err := os.WriteFile(src, data, 0600); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(d, "stale")
	if _, err := StageInspected(src, stale, original, nil, "", false); err == nil {
		t.Fatal("accepted source changed after inspection")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale stage left behind: %v", err)
	}
}
func TestMalformedLengths(t *testing.T) {
	cases := [][]byte{[]byte("fLaC"), append([]byte("fLaC"), []byte{128, 255, 255, 255}...), {0, 0, 0, 0}}
	for _, data := range cases {
		if _, _, err := readMetadata(bytes.NewReader(data)); err == nil {
			t.Fatalf("accepted %x", data)
		}
	}
	if _, _, err := decodeComments([]byte{255, 255, 255, 255}); err == nil {
		t.Fatal("accepted overflowing vendor")
	}
	if err := validatePicture(make([]byte, 4)); err == nil {
		t.Fatal("accepted short picture")
	}
}
func TestReplace(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "temp")
	dest := filepath.Join(d, "dest")
	os.WriteFile(src, []byte("new"), 0600)
	os.WriteFile(dest, []byte("old"), 0600)
	if err := Replace(src, dest); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "new" {
		t.Fatal("not replaced")
	}
}
func FuzzMetadata(f *testing.F) {
	f.Add([]byte("fLaC"))
	f.Add(append([]byte{'f', 'L', 'a', 'C', 128, 0, 0, 34}, make([]byte, 34)...))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > maxMetadata {
			return
		}
		readMetadata(bytes.NewReader(b))
	})
}
func FuzzComments(f *testing.F) {
	f.Add(encodeComments("v", []string{"TITLE=a"}))
	f.Fuzz(func(t *testing.T, b []byte) { decodeComments(b) })
}

func TestArtworkPreservedUnlessReviewed(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "a.flac")
	fixture(t, src)
	art := filepath.Join(d, "cover.png")
	f, err := os.Create(art)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	first := filepath.Join(d, "first")
	a, err := Stage(src, first, nil, art, false)
	if err != nil {
		t.Fatal(err)
	}
	if !a.HasArtwork {
		t.Fatal("picture missing")
	}
	b, err := Stage(first, filepath.Join(d, "second"), nil, filepath.Join(d, "missing.png"), false)
	if err != nil {
		t.Fatal("existing artwork should be preserved", err)
	}
	if !bytes.Equal(a.Metadata, b.Metadata) {
		t.Fatal("existing metadata changed")
	}
	if _, err = Stage(first, filepath.Join(d, "third"), nil, filepath.Join(d, "missing.png"), true); err == nil {
		t.Fatal("reviewed replacement missing file accepted")
	}
}
func TestEmptyDuplicateDoesNotDiscardPopulatedTag(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "a.flac")
	f, _ := os.Create(src)
	ff := flac.File{Meta: []*flac.MetaDataBlock{{Type: flac.StreamInfo, Data: make([]byte, 34)}, {Type: flac.VorbisComment, Data: encodeComments("v", []string{"ARTIST=Original", "ARTIST="})}}, Frames: bytes.NewReader([]byte{255, 248, 1})}
	ff.WriteTo(f)
	f.Close()
	got, err := Stage(src, filepath.Join(d, "stage"), map[string]string{"ARTIST": "new"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tags["ARTIST"] != "Original" {
		t.Fatal(got.Tags)
	}
}

// failingSink writes real bytes to the staging file up to its limit, then
// simulates a full disk or a broken writer.
type failingSink struct {
	*os.File
	remaining int
	writeErr  error
	syncErr   error
}

func (s *failingSink) Write(p []byte) (int, error) {
	if len(p) <= s.remaining {
		n, err := s.File.Write(p)
		s.remaining -= n
		return n, err
	}
	n, err := s.File.Write(p[:s.remaining])
	s.remaining -= n
	if err != nil {
		return n, err
	}
	return n, s.writeErr
}
func (s *failingSink) Sync() error {
	if s.syncErr != nil {
		return s.syncErr
	}
	return s.File.Sync()
}
func TestStageWriteFailuresCleanUpAndPreserveSource(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "source.flac")
	fixture(t, src)
	before, err := Inspect(src)
	if err != nil {
		t.Fatal(err)
	}
	blocks, _, err := readMetadata(bytes.NewReader(before.Metadata))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name                    string
		limit                   int
		writeErr, syncErr, want error
	}{
		{"disk_full_signature", 2, syscall.ENOSPC, nil, syscall.ENOSPC},
		{"disk_full_metadata", 25, syscall.ENOSPC, nil, syscall.ENOSPC},
		{"disk_full_audio", len(before.Metadata) + 3, syscall.ENOSPC, nil, syscall.ENOSPC},
		{"short_metadata", 25, nil, nil, io.ErrShortWrite},
		{"short_audio", len(before.Metadata) + 3, nil, nil, io.ErrShortWrite},
		{"sync_disk_full", 1 << 20, nil, syscall.ENOSPC, syscall.ENOSPC},
	} {
		t.Run(tt.name, func(t *testing.T) {
			temp := filepath.Join(d, tt.name)
			_, err := stageWithSink(src, temp, blocks, before, func(path string, mode os.FileMode) (stageSink, error) {
				f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
				if err != nil {
					return nil, err
				}
				return &failingSink{File: f, remaining: tt.limit, writeErr: tt.writeErr, syncErr: tt.syncErr}, nil
			})
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
			if _, err := os.Stat(temp); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed stage remains: %v", err)
			}
			after, err := Hash(src)
			if err != nil {
				t.Fatal(err)
			}
			if after != before.FileHash {
				t.Fatal("source altered by failed stage")
			}
		})
	}
}

func TestRealEncodedFLACRoundTrip(t *testing.T) {
	src := filepath.Join("..", "..", "tests", "testdata", "tone.flac")
	before, err := Inspect(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tagged := filepath.Join(dir, "tagged.flac")
	after, err := Stage(src, tagged, map[string]string{"ARTIST": "Fixture", "TITLE": "440 Hz sine"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if after.AudioHash != before.AudioHash || after.Tags["TITLE"] != "440 Hz sine" {
		t.Fatal("real FLAC tagging failed")
	}
	restored, err := StageRestore(tagged, filepath.Join(dir, "restored.flac"), before.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	if restored.FileHash != before.FileHash {
		t.Fatal("real FLAC restoration differs")
	}
}

func TestRestoreRefusesDifferentStreamInfo(t *testing.T) {
	src := filepath.Join("..", "..", "tests", "testdata", "tone.flac")
	before, err := Inspect(src)
	if err != nil {
		t.Fatal(err)
	}
	metadata := append([]byte(nil), before.Metadata...)
	metadata[8] ^= 1
	temp := filepath.Join(t.TempDir(), "restore.flac")
	if _, err = StageRestore(src, temp, metadata); err == nil {
		t.Fatal("accepted metadata describing different audio")
	}
	if _, err = os.Stat(temp); !os.IsNotExist(err) {
		t.Fatal("refused restore created an output")
	}
}
