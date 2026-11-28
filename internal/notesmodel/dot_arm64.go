package notesmodel

// dot requires equal input lengths that are multiples of 16.
//
//go:noescape
func dot(x, y []float32) float32
