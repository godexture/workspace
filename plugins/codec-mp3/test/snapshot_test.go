package test

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/godexture/codec-mp3/internal/mp3"
)

var testFiles = []string{
	"l1-fl4.mp3",
	"l2-fl13.mp3",
	"l3-he_32khz.mp3",
	"l3-hecommon.mp3",
	"l3-nonstandard-id3v2.mp3",
	"l3-sin1k0db.mp3",
}

// Step 4 (Go native implementation) などで、浮動小数点の丸め誤差を許容する場合は false にします。
const requireBitExact = false

const defaultMaxRMSE = 1e-3    // 波形全体のエネルギー誤差の許容値
const defaultMaxAbsDiff = 1e-2 // 局所的な（単一サンプルの）最大スパイク誤差の許容値

func TestSnapshots(t *testing.T) {
	for _, filename := range testFiles {
		t.Run(filename, func(t *testing.T) {
			mp3Path := filepath.Join("testdata", filename)
			mp3Data, err := os.ReadFile(mp3Path)
			if err != nil {
				t.Fatalf("failed to read test MP3 file: %v", err)
			}

			// Decode using minimp3 wrapper (or native implementation)
			pcm, err := decodeAll(mp3Data)
			if err != nil {
				t.Fatalf("failed to decode MP3 data: %v", err)
			}

			snapshotDir := filepath.Join("testdata", "snapshots")
			snapshotPath := filepath.Join(snapshotDir, filename+".snapshot")

			// Load snapshot and compare
			expectedPcm, err := loadSnapshot(snapshotPath)
			if err != nil {
				t.Fatalf("failed to load snapshot: %v", err)
			}

			if requireBitExact {
				if err := compareExact(pcm, expectedPcm); err != nil {
					t.Errorf("bit-exact comparison failed: %v", err)
				}
			} else {
				if err := comparePCM(pcm, expectedPcm, defaultMaxRMSE, defaultMaxAbsDiff); err != nil {
					t.Errorf("PCM comparison failed: %v", err)
				}
			}
		})
	}
}

func decodeAll(mp3Data []byte) ([]float32, error) {
	skipped := mp3.SkipId3(mp3Data)
	mp3Data = mp3Data[skipped:]

	var dec mp3.Mp3Dec
	dec.Init()

	var allPCM []float32
	pcmBuf := make([]float32, 1152*2)

	offset := 0
	for offset < len(mp3Data) {
		remaining := mp3Data[offset:]
		samples, info := dec.DecodeFrame(remaining, pcmBuf)
		if info.FrameBytes > 0 {
			if samples > 0 {
				decodedSamples := samples * info.Channels
				allPCM = append(allPCM, pcmBuf[:decodedSamples]...)
			}
			offset += info.FrameBytes
		} else {
			break
		}
	}
	return allPCM, nil
}

func loadSnapshot(path string) ([]float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var data []float32
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		val, err := strconv.ParseFloat(line, 32)
		if err != nil {
			return nil, fmt.Errorf("failed to parse float at index %d: %w", len(data), err)
		}
		data = append(data, float32(val))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func compareExact(actual, expected []float32) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("length mismatch: got %d, expected %d", len(actual), len(expected))
	}
	for i := range actual {
		if actual[i] != expected[i] {
			return fmt.Errorf("mismatch at index %d: got %f, expected %f", i, actual[i], expected[i])
		}
	}
	return nil
}

// comparePCM はRMSEと最大絶対誤差を用いてPCMデータを比較します。
func comparePCM(actual, expected []float32, maxRMSE, maxAbsDiff float64) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("length mismatch: got %d, expected %d", len(actual), len(expected))
	}

	if len(actual) == 0 {
		return nil
	}

	var sumSquares float64
	var maxDiff float64
	var maxDiffIdx int

	for i := range actual {
		// 計算精度を保つためにfloat64にキャストして評価します
		diff := math.Abs(float64(actual[i]) - float64(expected[i]))

		if diff > maxDiff {
			maxDiff = diff
			maxDiffIdx = i
		}

		sumSquares += diff * diff
	}

	rmse := math.Sqrt(sumSquares / float64(len(actual)))

	if rmse > maxRMSE {
		return fmt.Errorf("RMSE is too high: %e (max allowed: %e)", rmse, maxRMSE)
	}

	if maxDiff > maxAbsDiff {
		return fmt.Errorf("max absolute difference is too high: %e at index %d (got %f, expected %f) (max allowed: %e)",
			maxDiff, maxDiffIdx, actual[maxDiffIdx], expected[maxDiffIdx], maxAbsDiff)
	}

	return nil
}
