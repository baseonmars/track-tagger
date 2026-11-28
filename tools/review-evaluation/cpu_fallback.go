//go:build review_evaluation && !darwin && !linux

package main

func processCPU() (int64, bool) { return 0, false }
