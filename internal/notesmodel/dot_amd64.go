package notesmodel

import "golang.org/x/sys/cpu"

func dot(x, y []float32) float32 {
	if cpu.X86.HasAVX && cpu.X86.HasFMA {
		return vectorDot(x, y)
	}
	return scalarDot(x, y)
}

//go:noescape
func vectorDot(x, y []float32) float32
