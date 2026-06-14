package mp3

import (
	"math/rand"
	"testing"
)

func TestL3ImdctCWrapper(t *testing.T) {
	r := rand.New(rand.NewSource(12345))

	grbuf := make([]float32, 576)
	overlap := make([]float32, 9*32)
	for i := range grbuf {
		grbuf[i] = r.Float32()*2.0 - 1.0
	}
	for i := range overlap {
		overlap[i] = r.Float32()*2.0 - 1.0
	}

	// Call the function to verify CGO linkage and execution
	L3Imdct(grbuf, overlap, 0, 0)
}
