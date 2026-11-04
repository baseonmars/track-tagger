// noterole-features exposes the production feature implementation to offline
// training. It reads {line,prev,next} JSONL and emits versioned sparse counts.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"track-tagger/internal/noterole"
)

func main() {
	modelPath := flag.String("model", "", "optional passive artifact for scoring/parity")
	flag.Parse()
	var model *noterole.Model
	if *modelPath != "" {
		f, err := os.Open(*modelPath)
		if err != nil {
			fail(err)
		}
		model, err = noterole.Load(f)
		f.Close()
		if err != nil {
			fail(err)
		}
	}
	s := bufio.NewScanner(os.Stdin)
	s.Buffer(make([]byte, 4096), 1024*1024)
	e := json.NewEncoder(os.Stdout)
	for s.Scan() {
		var row struct {
			Line string `json:"line"`
			Prev string `json:"prev"`
			Next string `json:"next"`
		}
		if err := json.Unmarshal(s.Bytes(), &row); err != nil {
			fail(err)
		}
		if model != nil {
			suggestion, err := model.Suggest(row.Line, row.Prev, row.Next)
			if err != nil {
				fail(err)
			}
			if err = e.Encode(suggestion); err != nil {
				fail(err)
			}
			continue
		}
		f, err := noterole.Features(row.Line, row.Prev, row.Next)
		if err != nil {
			fail(err)
		}
		if err = e.Encode(struct {
			Version  string             `json:"feature_version"`
			Features map[string]float64 `json:"features"`
		}{noterole.FeatureVersion, f}); err != nil {
			fail(err)
		}
	}
	if err := s.Err(); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
