// noterole-benchmark measures passive model loading and complete source-note
// feature/scoring CPU time. It does not load labels or mutate catalog plans.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"
	"track-tagger/internal/noterole"
)

type note struct {
	Name string `json:"name"`
	Text string `json:"text"`
}
type input struct {
	ID    string `json:"id"`
	Notes []note `json:"notes"`
}
type timing struct {
	ID          string  `json:"id,omitempty"`
	CPUSeconds  float64 `json:"cpu_seconds"`
	WallSeconds float64 `json:"wall_seconds"`
	Lines       int     `json:"lines,omitempty"`
	Errors      int     `json:"errors,omitempty"`
	Abstained   int     `json:"abstained,omitempty"`
}

func main() {
	path := flag.String("model", "", "passive model path")
	loads := flag.Int("loads", 100, "bounded cold parser loads (warm filesystem)")
	flag.Parse()
	if *loads < 1 || *loads > 1000 {
		fail(fmt.Errorf("loads must be1..1000"))
	}
	var rows []input
	data, err := io.ReadAll(io.LimitReader(os.Stdin, (64<<20)+1))
	if err != nil {
		fail(err)
	}
	if len(data) > 64<<20 {
		fail(fmt.Errorf("benchmark input exceeds byte bound"))
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		fail(err)
	}
	if len(rows) > 1000 {
		fail(fmt.Errorf("record bound"))
	}
	cold := []timing{}
	var model *noterole.Model
	for i := 0; i < *loads; i++ {
		runtime.GC()
		c, err := cpuSeconds()
		if err != nil {
			fail(err)
		}
		start := time.Now()
		f, err := os.Open(*path)
		if err != nil {
			fail(err)
		}
		model, err = noterole.Load(f)
		f.Close()
		if err != nil {
			fail(err)
		}
		wall := time.Since(start).Seconds()
		end, err := cpuSeconds()
		if err != nil {
			fail(err)
		}
		cold = append(cold, timing{CPUSeconds: end - c, WallSeconds: wall})
	}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	warm := []timing{}
	for _, row := range rows {
		if len(row.Notes) > 1024 {
			fail(fmt.Errorf("note count bound"))
		}
		for _, n := range row.Notes {
			if len(n.Text) > 4<<20 {
				fail(fmt.Errorf("note byte bound"))
			}
		}
		c, err := cpuSeconds()
		if err != nil {
			fail(err)
		}
		start := time.Now()
		t := timing{ID: row.ID}
		for _, n := range row.Notes {
			lines, err := noterole.Lines(n.Text)
			if err != nil {
				t.Errors++
				continue
			}
			for i, line := range lines {
				prev, next := "", ""
				if i > 0 {
					prev = lines[i-1]
				}
				if i+1 < len(lines) {
					next = lines[i+1]
				}
				s, err := model.Suggest(line, prev, next)
				t.Lines++
				if err != nil {
					t.Errors++
				} else if s.Abstention != "" {
					t.Abstained++
				}
			}
		}
		t.WallSeconds = time.Since(start).Seconds()
		end, err := cpuSeconds()
		if err != nil {
			fail(err)
		}
		t.CPUSeconds = end - c
		warm = append(warm, t)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	info, err := os.Stat(*path)
	if err != nil {
		fail(err)
	}
	result := map[string]any{"schema_version": 1, "go_version": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH, "model_bytes": info.Size(), "cold_loads": cold, "warm_recordings": warm, "resident_heap_bytes_before_warm": before.HeapAlloc, "heap_bytes_after_warm": after.HeapAlloc, "warm_total_allocated_bytes": after.TotalAlloc - before.TotalAlloc, "measurement": "OS process user+system CPU; all note normalization/features/scoring included. JSON input decoding and IPC excluded; cold parser loads use warm filesystem cache. Source proposal eligibility guard measured separately."}
	if err = json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
