package internal_test

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
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
	updateSnapshots := os.Getenv("UPDATE_SNAPSHOTS") == "1"

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

			if updateSnapshots {
				if err := os.MkdirAll(snapshotDir, 0755); err != nil {
					t.Fatalf("failed to create snapshot directory: %v", err)
				}
				if err := saveSnapshot(snapshotPath, pcm); err != nil {
					t.Fatalf("failed to save snapshot: %v", err)
				}
				t.Logf("Updated snapshot for %s", filename)
				return
			}

			// Load snapshot and compare
			expectedPcm, err := loadSnapshot(snapshotPath)
			if err != nil {
				t.Fatalf("failed to load snapshot: %v (run UPDATE_SNAPSHOTS=1 go test to generate if missing)", err)
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

func saveSnapshot(path string, data []float32) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 4)
	for _, val := range data {
		bits := math.Float32bits(val)
		binary.LittleEndian.PutUint32(buf, bits)
		if _, err := f.Write(buf); err != nil {
			return err
		}
	}
	return nil
}

func loadSnapshot(path string) ([]float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	size := info.Size()
	if size%4 != 0 {
		return nil, fmt.Errorf("invalid snapshot file size: %d", size)
	}

	data := make([]float32, size/4)
	buf := make([]byte, 4)
	for i := range data {
		if _, err := io.ReadFull(f, buf); err != nil {
			return nil, err
		}
		bits := binary.LittleEndian.Uint32(buf)
		data[i] = math.Float32frombits(bits)
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
