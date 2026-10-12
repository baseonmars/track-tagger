// Package media provides bounded FLAC metadata editing with streaming audio copies.
package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	pic "github.com/go-flac/flacpicture/v2"
	flac "github.com/go-flac/go-flac/v2"
)

const maxMetadata = 64 << 20
const maxBlock = (1 << 24) - 1

var ErrWaiting = errors.New("file is not locally available or is incomplete")

type Info struct {
	Tags       map[string]string `json:"tags"`
	Metadata   []byte            `json:"-"`
	AudioHash  string            `json:"audio_hash"`
	FileHash   string            `json:"file_hash"`
	HasArtwork bool              `json:"has_artwork"`
}

func CheckLocal(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("not a regular local file: %s", path)
	}
	if st.Size() == 0 {
		return ErrWaiting
	}
	return checkPlatformLocal(path, st)
}

func Hash(path string) (string, error) {
	if err := CheckLocal(path); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// readMetadata validates sizes before calling library metadata constructors.
func readMetadata(r io.Reader) ([]*flac.MetaDataBlock, []byte, error) {
	var raw bytes.Buffer
	head := make([]byte, 4)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, nil, err
	}
	if string(head) != "fLaC" {
		return nil, nil, errors.New("missing FLAC signature")
	}
	raw.Write(head)
	var blocks []*flac.MetaDataBlock
	comments := false
	for len(blocks) < 4096 {
		if _, err := io.ReadFull(r, head); err != nil {
			return nil, nil, err
		}
		n := int(head[1])<<16 | int(head[2])<<8 | int(head[3])
		typ := flac.BlockType(head[0] & 127)
		if typ == flac.Invalid || raw.Len()+4+n > maxMetadata {
			return nil, nil, errors.New("invalid or oversized FLAC metadata")
		}
		if len(blocks) == 0 && (typ != flac.StreamInfo || n != 34) {
			return nil, nil, errors.New("FLAC STREAMINFO must be first and 34 bytes")
		}
		if len(blocks) > 0 && typ == flac.StreamInfo {
			return nil, nil, errors.New("duplicate STREAMINFO")
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, nil, err
		}
		raw.Write(head)
		raw.Write(data)
		if typ == flac.VorbisComment {
			if comments {
				return nil, nil, errors.New("duplicate Vorbis comments")
			}
			comments = true
			if _, _, err := decodeComments(data); err != nil {
				return nil, nil, err
			}
		}
		if typ == flac.Picture {
			if err := validatePicture(data); err != nil {
				return nil, nil, err
			}
		}
		blocks = append(blocks, &flac.MetaDataBlock{Type: typ, Data: data})
		if head[0]&128 != 0 {
			return blocks, raw.Bytes(), nil
		}
	}
	return nil, nil, errors.New("too many FLAC metadata blocks")
}
func sized(r *bytes.Reader, order binary.ByteOrder) ([]byte, error) {
	var n uint32
	if err := binary.Read(r, order, &n); err != nil {
		return nil, err
	}
	if uint64(n) > uint64(r.Len()) {
		return nil, io.ErrUnexpectedEOF
	}
	p := make([]byte, int(n))
	_, err := io.ReadFull(r, p)
	return p, err
}
func decodeComments(data []byte) (string, []string, error) {
	r := bytes.NewReader(data)
	v, err := sized(r, binary.LittleEndian)
	if err != nil {
		return "", nil, err
	}
	var n uint32
	if err = binary.Read(r, binary.LittleEndian, &n); err != nil {
		return "", nil, err
	}
	if uint64(n) > uint64(r.Len()/4) {
		return "", nil, io.ErrUnexpectedEOF
	}
	var vals []string
	for i := uint32(0); i < n; i++ {
		p, err := sized(r, binary.LittleEndian)
		if err != nil {
			return "", nil, err
		}
		if !utf8.Valid(p) {
			return "", nil, errors.New("invalid UTF-8 Vorbis comment")
		}
		vals = append(vals, string(p))
	}
	if r.Len() != 0 {
		return "", nil, errors.New("trailing Vorbis comment data")
	}
	return string(v), vals, nil
}
func validatePicture(data []byte) error {
	r := bytes.NewReader(data)
	if r.Len() < 4 {
		return io.ErrUnexpectedEOF
	}
	r.Seek(4, io.SeekStart)
	for i := 0; i < 2; i++ {
		if _, err := sized(r, binary.BigEndian); err != nil {
			return err
		}
	}
	if r.Len() < 16 {
		return io.ErrUnexpectedEOF
	}
	r.Seek(16, io.SeekCurrent)
	if _, err := sized(r, binary.BigEndian); err != nil {
		return err
	}
	if r.Len() != 0 {
		return errors.New("trailing picture data")
	}
	return nil
}
func encodeComments(vendor string, vals []string) []byte {
	var b bytes.Buffer
	put := func(s string) { binary.Write(&b, binary.LittleEndian, uint32(len(s))); b.WriteString(s) }
	put(vendor)
	binary.Write(&b, binary.LittleEndian, uint32(len(vals)))
	for _, v := range vals {
		put(v)
	}
	return b.Bytes()
}

// InspectMetadata reads and validates only the bounded FLAC metadata and checks
// that an audio payload exists. Hashes are deliberately absent; writes must use Inspect.
func InspectMetadata(path string) (Info, error) { return inspect(path, false) }

func Inspect(path string) (Info, error) { return inspect(path, true) }

func inspect(path string, hashes bool) (Info, error) {
	var info Info
	if err := CheckLocal(path); err != nil {
		return info, err
	}
	f, err := os.Open(path)
	if err != nil {
		return info, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return info, err
	}
	full := sha256.New()
	var r io.Reader = f
	if hashes {
		r = io.TeeReader(f, full)
	}
	blocks, raw, err := readMetadata(r)
	if err != nil {
		return info, err
	}
	info.Metadata = raw
	info.Tags = map[string]string{}
	for _, b := range blocks {
		if b.Type == flac.Picture {
			info.HasArtwork = true
		}
		if b.Type == flac.VorbisComment {
			_, vals, _ := decodeComments(b.Data)
			for _, v := range vals {
				k, val, ok := strings.Cut(v, "=")
				if ok {
					key := strings.ToUpper(k)
					if old := info.Tags[key]; old != "" && val != "" && old != val {
						info.Tags[key] = old + "; " + val
					} else if old == "" {
						info.Tags[key] = val
					}
				}
			}
		}
	}
	audio := sha256.New()
	var n int64
	if hashes {
		n, err = io.Copy(audio, r)
	} else {
		var sample [1]byte
		var count int
		count, err = r.Read(sample[:])
		n = int64(count)
		if err == io.EOF {
			err = nil
		}
	}
	if err != nil {
		return Info{}, err
	}
	if n == 0 {
		return Info{}, ErrWaiting
	}
	after, err := f.Stat()
	if err != nil {
		return Info{}, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return Info{}, errors.New("source changed during inspection")
	}
	if hashes {
		info.AudioHash = hex.EncodeToString(audio.Sum(nil))
		info.FileHash = hex.EncodeToString(full.Sum(nil))
	}
	return info, nil
}

// Stage creates temp exclusively. Existing populated values and pictures remain
// untouched unless replace was explicitly enabled by a reviewed decision.
func Stage(src, temp string, tags map[string]string, artwork string, replace bool) (Info, error) {
	original, err := Inspect(src)
	if err != nil {
		return Info{}, err
	}
	blocks, _, err := readMetadata(bytes.NewReader(original.Metadata))
	if err != nil {
		return Info{}, err
	}
	var comment *flac.MetaDataBlock
	vendor := "Track Tagger"
	var vals []string
	for _, b := range blocks {
		if b.Type == flac.VorbisComment {
			comment = b
			vendor, vals, _ = decodeComments(b.Data)
		}
	}
	keys := make([]string, 0, len(tags))
	normalized := map[string]string{}
	for k, v := range tags {
		k = strings.ToUpper(k)
		if k == "" || !utf8.ValidString(v) {
			return Info{}, errors.New("invalid tag")
		}
		for _, c := range k {
			if c < 32 || c > 125 || c == '=' {
				return Info{}, errors.New("invalid tag name")
			}
		}
		normalized[k] = v
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := normalized[key]
		if !replace && original.Tags[key] != "" {
			continue
		}
		out := vals[:0]
		for _, v := range vals {
			k, _, _ := strings.Cut(v, "=")
			if !strings.EqualFold(k, key) {
				out = append(out, v)
			}
		}
		vals = append(out, key+"="+value)
	}
	data := encodeComments(vendor, vals)
	if len(data) > maxBlock {
		return Info{}, errors.New("comments too large")
	}
	if comment != nil {
		comment.Data = data
	} else {
		blocks = append(blocks, &flac.MetaDataBlock{Type: flac.VorbisComment, Data: data})
	}
	if artwork != "" && (!original.HasArtwork || replace) {
		b, err := picture(artwork)
		if err != nil {
			return Info{}, err
		}
		if replace {
			out := blocks[:0]
			for _, old := range blocks {
				if old.Type != flac.Picture {
					out = append(out, old)
				}
			}
			blocks = out
		}
		blocks = append(blocks, b)
	}
	return stage(src, temp, blocks, original)
}
func picture(path string) (*flac.MetaDataBlock, error) {
	if err := CheckLocal(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBlock+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBlock-1024 {
		return nil, errors.New("artwork too large")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if format != "png" && format != "jpeg" {
		return nil, errors.New("only JPEG and PNG artwork supported")
	}
	p := pic.MetadataBlockPicture{PictureType: pic.PictureTypeFrontCover, MIME: "image/" + format, Width: uint32(cfg.Width), Height: uint32(cfg.Height), ColorDepth: 24, ImageData: data}
	b := p.Marshal()
	return &b, nil
}
func StageRestore(src, temp string, metadata []byte) (Info, error) {
	original, err := Inspect(src)
	if err != nil {
		return Info{}, err
	}
	blocks, raw, err := readMetadata(bytes.NewReader(metadata))
	if err != nil {
		return Info{}, err
	}
	if len(raw) != len(metadata) {
		return Info{}, errors.New("trailing restoration metadata")
	}
	if !bytes.Equal(blocks[0].Data, original.Metadata[8:42]) {
		return Info{}, errors.New("restoration STREAMINFO does not match the current audio")
	}
	return stage(src, temp, blocks, original)
}

// stageSink permits failure injection without shared mutable state. Production
// stages always create their destination exclusively through os.OpenFile.
type stageSink interface {
	io.Writer
	Sync() error
	Close() error
}
type sinkFactory func(string, os.FileMode) (stageSink, error)

// strictWriter turns a short write with a missing error into io.ErrShortWrite.
// go-flac's metadata writer checks errors but does not check each write count.
type strictWriter struct{ io.Writer }

func (w strictWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}
func writeFLAC(w io.Writer, blocks []*flac.MetaDataBlock, frames io.Reader) error {
	file := flac.File{Meta: blocks, Frames: frames}
	_, err := file.WriteTo(strictWriter{w})
	return err
}
func stage(src, temp string, blocks []*flac.MetaDataBlock, original Info) (Info, error) {
	return stageWithSink(src, temp, blocks, original, func(path string, mode os.FileMode) (stageSink, error) {
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	})
}
func stageWithSink(src, temp string, blocks []*flac.MetaDataBlock, original Info, openSink sinkFactory) (result Info, err error) {
	total := 4
	for _, b := range blocks {
		total += 4 + len(b.Data)
		if len(b.Data) > maxBlock || total > maxMetadata {
			return Info{}, errors.New("metadata too large")
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return result, err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return result, err
	}
	if _, err = in.Seek(int64(len(original.Metadata)), io.SeekStart); err != nil {
		return result, err
	}
	out, err := openSink(temp, st.Mode().Perm())
	if err != nil {
		return result, err
	}
	defer func() {
		out.Close()
		if err != nil {
			os.Remove(temp)
		}
	}()
	if err = writeFLAC(out, blocks, in); err != nil {
		return result, err
	}
	if err = out.Sync(); err != nil {
		return result, err
	}
	if err = out.Close(); err != nil {
		return result, err
	}
	result, err = Inspect(temp)
	if err != nil {
		return result, err
	}
	if result.AudioHash != original.AudioHash {
		return Info{}, errors.New("audio payload changed during staging")
	}
	current, err := Hash(src)
	if err != nil {
		return Info{}, err
	}
	if current != original.FileHash {
		return Info{}, errors.New("source changed during staging")
	}
	return result, nil
}
