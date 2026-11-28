//go:build darwin || linux

package notesmodel

import (
	"bufio"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func processCPU(t *testing.T) time.Duration {
	t.Helper()
	var result unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_PROCESS_CPUTIME_ID, &result); err != nil {
		t.Fatal(err)
	}
	return time.Duration(result.Nano())
}

func TestCPULatency(t *testing.T) {
	path := os.Getenv("TRACK_TAGGER_NOTES_MODEL_PARITY_INPUT")
	if path == "" {
		t.Skip("private source fixture is optional")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var cases []referenceCase
	for scanner.Scan() {
		var row referenceCase
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty reference")
	}
	wallStart, cpuStart := time.Now(), processCPU(t)
	model, err := Load(testPack)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("asset load wall=%s CPU=%s", time.Since(wallStart), processCPU(t)-cpuStart)
	if _, err := model.Predict(cases[0].Notes); err != nil {
		t.Fatal(err)
	}
	wall := make([]time.Duration, 0, 3*len(cases))
	cpu := make([]time.Duration, 0, cap(wall))
	for range 3 {
		for _, c := range cases {
			wallStart, cpuStart := time.Now(), processCPU(t)
			_, err := model.Predict(c.Notes)
			cpu = append(cpu, processCPU(t)-cpuStart)
			wall = append(wall, time.Since(wallStart))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, measurement := range []struct {
		label   string
		samples []time.Duration
	}{{"CPU", cpu}, {"wall", wall}} {
		slices.Sort(measurement.samples)
		n := len(measurement.samples)
		t.Logf("%d samples %s p50=%s p95=%s p99=%s", n, measurement.label,
			measurement.samples[(n*50+99)/100-1], measurement.samples[(n*95+99)/100-1],
			measurement.samples[(n*99+99)/100-1])
	}
}
