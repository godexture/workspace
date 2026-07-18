//go:build goexperiment.simd && amd64

package mp3

import (
	"math/rand/v2"
	"testing"

	"github.com/godexture/sdk/dsp"
)

func TestSynthWindowSIMD(t *testing.T) {
	if !dsp.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	workspace := make([]float32, 2112)
	for i := range workspace {
		workspace[i] = rand.Float32()*2 - 1
	}
	for index := 0; index < 15; index++ {
		window := synthesizeWindowTable[index*16 : index*16+16]
		wantA, wantB := synthWindowScalar(workspace, 15*64, 14-index, window)
		gotA, gotB := synthWindowSIMD(workspace, 15*64, 14-index, window)
		if gotA != wantA || gotB != wantB {
			t.Fatalf("index %d: got %v/%v, want %v/%v", index, gotA, gotB, wantA, wantB)
		}
	}
}
