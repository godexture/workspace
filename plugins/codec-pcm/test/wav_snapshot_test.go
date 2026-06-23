//go:generate go run ../../../test/snapshot-generator .

package test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/sdk/testutil"
)

func TestWavDecodeSnapshots(t *testing.T) {
	for _, cfg := range configs {
		t.Run(cfg.name, func(t *testing.T) {
			t.Parallel()

			wavPath := filepath.Join("testdata", fmt.Sprintf("short_%s.wav", cfg.name))
			snapshotPath := filepath.Join("testdata", "snapshots", fmt.Sprintf("short_%s.snapshot", cfg.name))

			wavBytes, err := os.ReadFile(wavPath)
			if err != nil {
				t.Fatalf("failed to read WAV file: %v", err)
			}

			actualPCM, err := decodeWAVToPCM(wavBytes)
			if err != nil {
				t.Fatalf("failed to decode WAV to PCM: %v", err)
			}

			expectedPCM, err := testutil.LoadSnapshot(snapshotPath)
			if err != nil {
				t.Fatalf("failed to load snapshot: %v", err)
			}

			if len(actualPCM) < len(expectedPCM) {
				t.Fatalf("decoded PCM is shorter than expected snapshot: got %d, expected at least %d", len(actualPCM), len(expectedPCM))
			}
			actualPCM = actualPCM[:len(expectedPCM)]

			if err := testutil.ComparePCM(actualPCM, expectedPCM, cfg.maxDiff); err != nil {
				t.Errorf("PCM comparison failed: %v", err)
			}
		})
	}
}

func TestWavEncodeSnapshots(t *testing.T) {
	// Generate source PCM: 0.5 seconds at 16000Hz stereo
	srcPCM := generateSineWave(16000, 2, 0.5)

	for _, cfg := range configs {
		t.Run(cfg.name, func(t *testing.T) {
			t.Parallel()

			wavPath := filepath.Join("testdata", fmt.Sprintf("short_%s.wav", cfg.name))

			expectedWavBytes, err := os.ReadFile(wavPath)
			if err != nil {
				t.Fatalf("failed to read reference WAV file: %v", err)
			}

			pcm := srcPCM
			if cfg.channelLayout.ChannelCount() == 1 {
				pcm = downmixToMono(pcm)
			}
			if cfg.sampleRate != 16000 {
				pcm = resample16kTo8k(pcm, cfg.channelLayout.ChannelCount())
			}

			actualWavBytes, err := encodePCMToWAV(pcm, cfg.codecID, cfg.sampleRate, cfg.channelLayout, cfg.format)
			if err != nil {
				t.Fatalf("failed to encode PCM: %v", err)
			}

			if !bytes.Equal(actualWavBytes, expectedWavBytes) {
				t.Errorf("encoded WAV bytes mismatch from snapshot reference short_%s.wav (got %d bytes, want %d bytes)",
					cfg.name, len(actualWavBytes), len(expectedWavBytes))
			}
		})
	}
}
