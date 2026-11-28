// Package notesmodel runs the two frozen Tiny BERT span models in process.
// It is independent of the catalog and has no network or process-launch code.
package notesmodel

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
)

const (
	packSize      = 7569156
	packSHA       = "222c8047fa59c2ba98649e127f685405f124cf363d37b81274b96d4b50f36bd9"
	vocabSize     = 30522
	width         = 128
	intermediate  = 512
	positionCount = 512
	maxNotes      = 16
	maxWindows    = 40
)

var fields = [...]string{"artist", "date", "venue", "city"}

// Span identifies a verbatim source quote. Line and Unicode codepoint offsets
// are zero-based, and End is exclusive.
type Span struct {
	Field string `json:"field"`
	Note  string `json:"note"`
	Line  int    `json:"line"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// Model holds the immutable parameters for the LoRA and full-adaptation arms.
type Model struct {
	tokenizer tokenizer
	encoders  [2]encoder
}

type dense struct {
	weights, bias []float32
	in, out       int
}

type normalization struct {
	weight, bias []float32
}

type layer struct {
	query, key, value, project, expand, contract dense
	attentionNorm, outputNorm                    normalization
}

type encoder struct {
	tokens                       []byte
	scales, positions, tokenType []float32
	embeddingsNorm               normalization
	layers                       [2]layer
	head                         dense
	thresholds                   []float32
}

// Load verifies and loads the approved data pack. Callers can share a Model
// across concurrent predictions.
func Load(path string) (*Model, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != packSize {
		return nil, errors.New("unrecognized notes model pack size or file type")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	pack, err := io.ReadAll(io.LimitReader(file, packSize+1))
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(pack)) != packSHA {
		return nil, errors.New("notes model pack hash mismatch")
	}
	compressed, err := gzip.NewReader(bytes.NewReader(pack))
	if err != nil {
		return nil, err
	}
	defer compressed.Close()
	// The exact approved pack is hash-bound. The cap also limits decompression.
	data, err := io.ReadAll(io.LimitReader(compressed, 20*1024*1024))
	if err != nil {
		return nil, err
	}
	return decodePack(data), nil
}

type decoder struct {
	bytes []byte
}

func (d *decoder) take(n int) []byte {
	data := d.bytes[:n]
	d.bytes = d.bytes[n:]
	return data
}

func (d *decoder) floats(n int) []float32 {
	data := d.take(4 * n)
	out := make([]float32, n)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[4*i:]))
	}
	return out
}

func (d *decoder) linear(in, out int) dense {
	return dense{weights: d.floats(in * out), bias: d.floats(out), in: in, out: out}
}

func (d *decoder) norm() normalization {
	return normalization{weight: d.floats(width), bias: d.floats(width)}
}

// Only the byte-for-byte verified pack reaches this function.
func decodePack(data []byte) *Model {
	d := decoder{bytes: data}
	d.take(8) // TTBERT01
	size := int(binary.LittleEndian.Uint32(d.take(4)))
	vocabulary := strings.Split(strings.TrimSuffix(string(d.take(size)), "\n"), "\n")
	words := d.take(vocabSize * width)
	delta := d.take(len(words))
	base := append([]byte(nil), words...)
	full := make([]byte, len(base))
	for i, value := range words {
		full[i] = byte(int(int8(value)) + int(int8(delta[i])))
	}
	model := &Model{tokenizer: newTokenizer(vocabulary)}
	for variant, tokens := range [][]byte{base, full} {
		e := &model.encoders[variant]
		e.tokens = tokens
		e.thresholds = d.floats(len(fields))
		e.scales = d.floats(vocabSize)
		e.positions = d.floats(positionCount * width)
		e.tokenType = d.floats(width)
		e.embeddingsNorm = d.norm()
		for i := range e.layers {
			e.layers[i] = layer{
				query: d.linear(width, width), key: d.linear(width, width), value: d.linear(width, width),
				project: d.linear(width, width), expand: d.linear(width, intermediate),
				contract: d.linear(intermediate, width), attentionNorm: d.norm(), outputNorm: d.norm(),
			}
		}
		e.head = d.linear(width, 2*len(fields))
	}
	return model
}

// Predict returns only exact role and source-span agreement. The caller remains
// responsible for date semantics, source conflicts, and reviewer eligibility.
func (m *Model) Predict(notes map[string][]string) ([]Span, error) {
	return m.PredictContext(context.Background(), notes)
}

// PredictContext stops scoring when the caller cancels between source windows.
func (m *Model) PredictContext(ctx context.Context, notes map[string][]string) ([]Span, error) {
	windows, err := m.windows(ctx, notes)
	if err != nil {
		return nil, err
	}
	a, err := m.variantSpans(ctx, windows, 0)
	if err != nil {
		return nil, err
	}
	b, err := m.variantSpans(ctx, windows, 1)
	if err != nil {
		return nil, err
	}
	other := make(map[Span]bool, len(b))
	for _, span := range b {
		other[span] = true
	}
	var agreed []Span
	for _, span := range a {
		if other[span] {
			agreed = append(agreed, span)
		}
	}
	return agreed, nil
}

func (m *Model) windows(ctx context.Context, notes map[string][]string) ([]window, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(notes) > maxNotes {
		return nil, fmt.Errorf("notes model supports at most %d notes per recording", maxNotes)
	}
	names := make([]string, 0, len(notes))
	for name := range notes {
		names = append(names, name)
	}
	sort.Strings(names)
	var windows []window
	for _, name := range names {
		lines := notes[name]
		count := 0
		for index, line := range lines {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// A truncated header could hide another field or a section
			// boundary. End this note's header before interpreting it.
			if len(line) > maxLineBytes {
				break
			}
			text := strings.TrimSpace(line)
			if text == "" {
				if count > 0 {
					break
				}
				// Bound absent headers in unusually large notes.
				if index >= 128 {
					break
				}
				continue
			}
			count++
			if count > 10 || (section.MatchString(text) && !dateLike(text)) {
				break
			}
			if unsafeText.MatchString(text) {
				continue
			}
			value, ok := m.tokenizer.window(name, lines, index)
			if !ok {
				continue
			}
			if len(windows) == maxWindows {
				return nil, fmt.Errorf("notes model supports at most %d candidate lines per recording", maxWindows)
			}
			windows = append(windows, value)
		}
	}
	return windows, nil
}
