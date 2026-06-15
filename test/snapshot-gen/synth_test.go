//go:build cgo_test

package main

import (
	"math/rand"
	"testing"

	"github.com/godexture/codec-mp3/internal/mp3"
)

func TestGoSynthFilterMatchesC(t *testing.T) {
	r := rand.New(rand.NewSource(42))

	channelCountOptions := []int{1, 2}
	numberOfBandsOptions := []int{12, 18}

	for _, channelCount := range channelCountOptions {
		for _, numberOfBands := range numberOfBandsOptions {
			quadratureMirrorFilterStateC := make([]float32, 15*64)
			quadratureMirrorFilterStateGo := make([]float32, 15*64)
			for i := range quadratureMirrorFilterStateC {
				val := r.Float32()*2.0 - 1.0
				quadratureMirrorFilterStateC[i] = val
				quadratureMirrorFilterStateGo[i] = val
			}

			granuleBufferC := make([]float32, 2*576)
			granuleBufferGo := make([]float32, 2*576)
			for i := range granuleBufferC {
				val := r.Float32()*2.0 - 1.0
				granuleBufferC[i] = val
				granuleBufferGo[i] = val
			}

			synthesisWorkspaceC := make([]float32, 33*64)
			synthesisWorkspaceGo := make([]float32, 33*64)

			pcmSamplesC := make([]int16, 1152*2)
			pcmSamplesGoFloat := make([]float32, 1152*2)
			pcmSamplesGo := make([]int16, 1152*2)

			// Call C version via helper in minimp3.go
			C_synth_granule(quadratureMirrorFilterStateC, granuleBufferC, numberOfBands, channelCount, pcmSamplesC, synthesisWorkspaceC)

			// Call Go version
			mp3.SynthesizeGranule(quadratureMirrorFilterStateGo, granuleBufferGo, numberOfBands, channelCount, pcmSamplesGoFloat, synthesisWorkspaceGo)

			// Convert Go float32 samples to int16 samples
			mp3.ConvertFloat32ToSigned16BitPCMSamples(pcmSamplesGoFloat, pcmSamplesGo)

			for i := range pcmSamplesGo {
				diff := pcmSamplesGo[i] - pcmSamplesC[i]
				// Allow minor difference of 1 due to rounding differences in Float32 to Int16
				if diff < -1 || diff > 1 {
					t.Fatalf("PCM mismatch for channelCount=%d, numberOfBands=%d at index %d: Go=%d, C=%d (diff: %d)", channelCount, numberOfBands, i, pcmSamplesGo[i], pcmSamplesC[i], diff)
				}
			}

			for i := range quadratureMirrorFilterStateC {
				diff := quadratureMirrorFilterStateGo[i] - quadratureMirrorFilterStateC[i]
				if diff < -1e-5 || diff > 1e-5 {
					t.Fatalf("QMF state mismatch for channelCount=%d, numberOfBands=%d at index %d: Go=%f, C=%f (diff: %e)", channelCount, numberOfBands, i, quadratureMirrorFilterStateGo[i], quadratureMirrorFilterStateC[i], diff)
				}
			}
		}
	}
}
