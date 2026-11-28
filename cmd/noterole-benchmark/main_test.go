package main

import (
	"testing"
	"time"
)

func TestCPUClockMonotonic(t *testing.T) {
	a, err := cpuSeconds()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Millisecond)
	for time.Now().Before(deadline) {
	}
	b, err := cpuSeconds()
	if err != nil || b < a {
		t.Fatal(a, b, err)
	}
}
