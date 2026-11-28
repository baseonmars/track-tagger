//go:build !arm64

package notesmodel

func scalarDot(x, y []float32) float32 {
	var sum [8]float32
	for i := 0; i < len(x); i += 8 {
		a, b := x[i:i+8], y[i:i+8]
		sum[0] += a[0] * b[0]
		sum[1] += a[1] * b[1]
		sum[2] += a[2] * b[2]
		sum[3] += a[3] * b[3]
		sum[4] += a[4] * b[4]
		sum[5] += a[5] * b[5]
		sum[6] += a[6] * b[6]
		sum[7] += a[7] * b[7]
	}
	return (sum[0] + sum[1]) + (sum[2] + sum[3]) + (sum[4] + sum[5]) + (sum[6] + sum[7])
}
