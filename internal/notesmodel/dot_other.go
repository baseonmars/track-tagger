//go:build !arm64 && !amd64

package notesmodel

func dot(x, y []float32) float32 {
	return scalarDot(x, y)
}
