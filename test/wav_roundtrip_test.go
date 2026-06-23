package test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/sdk/testutil"
)

func TestWavRoundtripDemuxDecodeEncodeMux(t *testing.T) {
	for _, cfg := range configs {
		t.Run(cfg.name, func(t *testing.T) {
			t.Parallel()

			wavPath := filepath.Join("testdata", fmt.Sprintf("short_%s.wav", cfg.name))

			// Read original WAV bytes
			originalWavBytes, err := os.ReadFile(wavPath)
			if err != nil {
				t.Fatalf("failed to read WAV file: %v", err)
			}

			// Pass 1: Demux & Decode -> PCM 1
			pcm1, err := decodeWAVToPCM(originalWavBytes)
			if err != nil {
				t.Fatalf("failed to decode original WAV: %v", err)
			}

			// Pass 2: Encode & Mux -> WAV 2
			wavBytes2, err := encodePCMToWAV(pcm1, cfg.codecID, cfg.sampleRate, cfg.channelLayout, cfg.format)
			if err != nil {
				t.Fatalf("failed to encode PCM 1: %v", err)
			}

			// Pass 3: Demux & Decode -> PCM 2
			pcm2, err := decodeWAVToPCM(wavBytes2)
			if err != nil {
				t.Fatalf("failed to decode intermediate WAV: %v", err)
			}

			// Compare PCM 2 with PCM 1 (should be very close or identical)
			if len(pcm2) < len(pcm1) {
				t.Fatalf("length mismatch: got %d, expected at least %d", len(pcm2), len(pcm1))
			}
			pcm2 = pcm2[:len(pcm1)]

			if err := testutil.ComparePCM(pcm2, pcm1, cfg.maxDiff); err != nil {
				t.Errorf("PCM degradation check failed for roundtrip 1: %v", err)
			}
		})
	}
}

func TestWavRoundtripEncodeMuxDemuxDecode(t *testing.T) {
	// Generate source PCM: 0.5 seconds at 16000Hz stereo
	srcPCM := generateSineWave(16000, 2, 0.5)

	for _, cfg := range configs {
		t.Run(cfg.name, func(t *testing.T) {
			t.Parallel()

			// Resample/reformat input PCM if necessary (e.g. downmix to mono for PCMU/PCMA)
			pcm := srcPCM
			if cfg.channelLayout.ChannelCount() == 1 {
				pcm = downmixToMono(pcm)
			}
			if cfg.sampleRate != 16000 {
				pcm = resample16kTo8k(pcm, cfg.channelLayout.ChannelCount())
			}

			// Pass 1: Encode & Mux -> WAV
			wavBytes, err := encodePCMToWAV(pcm, cfg.codecID, cfg.sampleRate, cfg.channelLayout, cfg.format)
			if err != nil {
				t.Fatalf("failed to encode source PCM: %v", err)
			}

			// Pass 2: Demux & Decode -> Decoded PCM
			decodedPCM, err := decodeWAVToPCM(wavBytes)
			if err != nil {
				t.Fatalf("failed to decode WAV bytes: %v", err)
			}

			// Compare Decoded PCM with original source PCM
			if len(decodedPCM) < len(pcm) {
				t.Fatalf("length mismatch: got %d, expected at least %d", len(decodedPCM), len(pcm))
			}
			decodedPCM = decodedPCM[:len(pcm)]

			if err := testutil.ComparePCM(decodedPCM, pcm, cfg.maxDiff); err != nil {
				t.Errorf("PCM degradation check failed for roundtrip 2: %v", err)
			}
		})
	}
}
