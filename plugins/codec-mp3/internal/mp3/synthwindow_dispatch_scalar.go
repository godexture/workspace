//go:build !goexperiment.simd || !amd64

package mp3

func synthWindow(workspace []float32, zLineOffset, index int, window []float32) ([4]float32, [4]float32) {
	return synthWindowScalar(workspace, zLineOffset, index, window)
}
