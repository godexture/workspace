//go:build cgo_test

package main

import (
	"math/rand"
	"testing"

	"github.com/godexture/codec-mp3/internal/mp3"
)

func TestGoSynthFilterMatchesC(t *testing.T) {
	r := rand.New(rand.NewSource(42))

	nchOptions := []int{1, 2}
	nbandsOptions := []int{12, 18}

	for _, nch := range nchOptions {
		for _, nbands := range nbandsOptions {
			qmfStateC := make([]float32, 15*64)
			qmfStateGo := make([]float32, 15*64)
			for i := range qmfStateC {
				val := r.Float32()*2.0 - 1.0
				qmfStateC[i] = val
				qmfStateGo[i] = val
			}

			grbufC := make([]float32, 2*576)
			grbufGo := make([]float32, 2*576)
			for i := range grbufC {
				val := r.Float32()*2.0 - 1.0
				grbufC[i] = val
				grbufGo[i] = val
			}

			linsC := make([]float32, 33*64)
			linsGo := make([]float32, 33*64)

			pcmC := make([]float32, 1152*2)
			pcmGo := make([]float32, 1152*2)

			// Call C version via helper in minimp3.go
			C_synth_granule(qmfStateC, grbufC, nbands, nch, pcmC, linsC)

			// Call Go version
			mp3.Mp3dSynthGranuleFloat(qmfStateGo, grbufGo, nbands, nch, pcmGo, 0, linsGo)

			for i := range pcmC {
				diff := pcmGo[i] - pcmC[i]
				if diff < -1e-5 || diff > 1e-5 {
					t.Fatalf("PCM mismatch for nch=%d, nbands=%d at index %d: Go=%f, C=%f (diff: %e)", nch, nbands, i, pcmGo[i], pcmC[i], diff)
				}
			}

			for i := range qmfStateC {
				diff := qmfStateGo[i] - qmfStateC[i]
				if diff < -1e-5 || diff > 1e-5 {
					t.Fatalf("QMF state mismatch for nch=%d, nbands=%d at index %d: Go=%f, C=%f (diff: %e)", nch, nbands, i, qmfStateGo[i], qmfStateC[i], diff)
				}
			}
		}
	}
}
