package notesmodel

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

const testPack = "assets/bert-agreement.bin.gz"

func TestPackAndAgreement(t *testing.T) {
	model, err := Load(testPack)
	if err != nil {
		t.Fatal(err)
	}
	input := map[string][]string{
		"note.txt": {"Cloudglass Trio", "2004-03-26", "The Blue Vault", "Moonside, RI", "", "Source: audience recording."},
	}
	agreed, err := model.Predict(input)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(agreed, Span{"date", "note.txt", 1, 0, 10}) {
		t.Fatalf("missing exact date span: %v", agreed)
	}
	input["note.txt"][1] = "28/03/2004"
	uk, err := model.Predict(input)
	if err != nil || !slices.Contains(uk, Span{"date", "note.txt", 1, 0, 10}) {
		t.Fatalf("day-first quote did not reach downstream date validation: %v (%v)", uk, err)
	}
	for _, span := range agreed {
		line := []rune(input[span.Note][span.Line])
		if span.Start < 0 || span.Start >= span.End || span.End > len(line) {
			t.Fatalf("invalid source span: %v", span)
		}
	}
	if invalid, err := model.Predict(map[string][]string{"note.txt": {"Source: technical archive", "2004-03-26"}}); err != nil || len(invalid) != 0 {
		t.Fatalf("non-header should abstain: %v (%v)", invalid, err)
	}
}

func TestCancellationAndOversizedHeader(t *testing.T) {
	model, err := Load(testPack)
	if err != nil {
		t.Fatal(err)
	}
	notes := map[string][]string{"note.txt": {strings.Repeat("x", maxLineBytes+1), "2004-03-26"}}
	windows, err := model.windows(t.Context(), notes)
	if err != nil || len(windows) != 0 {
		t.Fatalf("oversized header should have no windows: %d (%v)", len(windows), err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := model.PredictContext(ctx, notes); !errors.Is(err, context.Canceled) || len(got) != 0 {
		t.Fatalf("canceled prediction = %v (%v)", got, err)
	}
}

func TestDayFirstDateCandidate(t *testing.T) {
	for _, quote := range []string{"28/03/2004", "12th September 2004", "7 March 2024"} {
		if !validQuote([]rune(quote), "date", 0, len([]rune(quote))) {
			t.Errorf("date candidate was discarded: %s", quote)
		}
	}
}

func TestDot(t *testing.T) {
	x, y := make([]float32, 128), make([]float32, 128)
	var want float32
	for i := range x {
		x[i], y[i] = float32(i-12), float32(10-i)
		want += x[i] * y[i]
	}
	if got := dot(x, y); got != want {
		t.Fatalf("dot = %f, want %f", got, want)
	}
}

func TestPackRejectsUnverifiedData(t *testing.T) {
	source := t.TempDir() + "/fake.gz"
	data := make([]byte, packSize)
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(source); err == nil {
		t.Fatal("accepted a correctly sized unverified model file")
	}
}

type referenceCase struct {
	ID       string
	Notes    map[string][]string
	Variants [2][]Span
}

func TestFrozenReference(t *testing.T) {
	path := os.Getenv("TRACK_TAGGER_NOTES_MODEL_PARITY_INPUT")
	if path == "" {
		t.Skip("private source fixture is optional")
	}
	model, err := Load(testPack)
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var counts [2]int
	var dateDifferences [2]int
	var durations []time.Duration
	var agreedCount int
	for scanner.Scan() {
		var row referenceCase
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		windows, err := model.windows(t.Context(), row.Notes)
		if err != nil {
			t.Fatal(err)
		}
		for i := range row.Variants {
			got, err := model.variantSpans(t.Context(), windows, i)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, row.Variants[i]) {
				missing := subtract(row.Variants[i], got)
				added := subtract(got, row.Variants[i])
				onlyAddedDate := len(missing) == 0
				for _, span := range added {
					onlyAddedDate = onlyAddedDate && span.Field == "date"
				}
				if onlyAddedDate {
					dateDifferences[i] += len(added)
					t.Logf("%s arm %d added date-like candidates %+v", row.ID, i, added)
				} else {
					t.Errorf("%s arm %d no longer matches reference: missing=%+v added=%+v", row.ID, i, missing, added)
				}
			}
			counts[i] += len(got)
		}
		start := time.Now()
		spans, err := model.Predict(row.Notes)
		durations = append(durations, time.Since(start))
		if err != nil {
			t.Fatal(err)
		}
		expected := subtract(row.Variants[0], subtract(row.Variants[0], row.Variants[1]))
		if missing := subtract(expected, spans); len(missing) != 0 {
			t.Errorf("%s missing agreed spans: %+v", row.ID, missing)
		}
		for _, span := range subtract(spans, expected) {
			if span.Field != "date" {
				t.Errorf("%s unexpected non-date agreed span: %+v", row.ID, span)
			}
		}
		agreedCount += len(spans)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(durations) == 0 {
		t.Fatal("empty reference")
	}
	slices.Sort(durations)
	t.Logf("%s", fmt.Sprintf("recordings=%d spans=%v added date spans=%v agreement=%d p50=%s p95=%s",
		len(durations), counts, dateDifferences, agreedCount, durations[(len(durations)-1)/2], durations[(len(durations)*95+99)/100-1]))
}

func subtract(source, other []Span) []Span {
	var result []Span
	for _, value := range source {
		if !slices.Contains(other, value) {
			result = append(result, value)
		}
	}
	return result
}

func TestUnicodeWordPieceOffsets(t *testing.T) {
	model, err := Load(testPack)
	if err != nil {
		t.Fatal(err)
	}
	pieces := model.tokenizer.encode([]rune("Écho — São"))
	var spans []offset
	for _, p := range pieces {
		spans = append(spans, p.offset)
	}
	want := []offset{{0, 4}, {5, 6}, {7, 10}}
	if !reflect.DeepEqual(spans, want) {
		t.Fatalf("offsets = %+v, want %+v", spans, want)
	}
}
