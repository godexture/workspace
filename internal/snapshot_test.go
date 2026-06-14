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

// Set to false when we reach Step 4 (Go native implementation) if float rounding causes slight differences.
const requireBitExact = true
const defaultEpsilon = 1e-5

func TestSnapshots(t *testing.T) {
	updateSnapshots := os.Getenv("UPDATE_SNAPSHOTS") == "1"

	for _, filename := range testFiles {
		t.Run(filename, func(t *testing.T) {
			mp3Path := filepath.Join("testdata", filename)
			mp3Data, err := os.ReadFile(mp3Path)
			if err != nil {
				t.Fatalf("failed to read test MP3 file: %v", err)
			}

			// Decode using minimp3 wrapper
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
				if err := compareEpsilon(pcm, expectedPcm, defaultEpsilon); err != nil {
					t.Errorf("epsilon comparison failed: %v", err)
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

func compareEpsilon(actual, expected []float32, epsilon float32) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("length mismatch: got %d, expected %d", len(actual), len(expected))
	}
	maxDiff := float32(0)
	for i := range actual {
		diff := float32(math.Abs(float64(actual[i] - expected[i])))
		if diff > epsilon {
			return fmt.Errorf("mismatch at index %d exceeds epsilon: got %f, expected %f (diff: %e, limit: %e)", i, actual[i], expected[i], diff, epsilon)
		}
		if diff > maxDiff {
			maxDiff = diff
		}
	}
	return nil
}
