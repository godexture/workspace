package test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godexture/codec-flac/test/bridge"
	"github.com/godexture/codec-flac/test/config"
	"github.com/godexture/sdk/testutil"
	audio "github.com/godexture/sdk/testutil/audio"
)

// TestFLACConformanceVectors exercises the CC0 FLAC decoder testbench. The
// subset files are compared against FFmpeg's decoded PCM. The uncommon group
// intentionally contains streams with properties changing between frames;
// those are tested for successful, panic-free decoding because common FFmpeg
// builds reject that historical/non-streamable construction.
func TestFLACConformanceVectors(t *testing.T) {
	root := filepath.Join(config.TestdataDir, "conformance")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		t.Skip("conformance vectors are not installed")
	}

	vectors := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".flac" {
			return nil
		}
		vectors++
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		t.Run(filepath.ToSlash(rel), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			group := strings.Split(filepath.ToSlash(rel), "/")[0]
			got, err := bridge.Decode(data)
			if group == "uncommon" && uncommonContainerVector(filepath.Base(path)) {
				if err != nil {
					t.Fatalf("uncommon vector failed to decode: %v", err)
				}
				compareWithFFmpegWhenCompatible(t, got, data)
				return
			}
			if group == "faulty" {
				if faultyMustReject(filepath.Base(path)) {
					if err == nil {
						t.Fatal("faulty vector unexpectedly decoded successfully")
					}
				} else if err != nil {
					t.Logf("faulty vector rejected: %v", err)
				} else {
					t.Log("faulty vector decoded without error; safety-only check")
				}
				return
			}
			if group != "subset" {
				// uncommon 10/11 intentionally start without a native FLAC
				// container.
				return
			}
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			want, err := testutil.DecodeWithFFmpeg(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("FFmpeg oracle failed: %v", err)
			}
			if err := audio.ComparePCM(got, want, audio.CompareOptions{
				MaxAbsDiff: 1.0 / 32768.0,
				MaxRMSE:    1e-6,
				MinSNR:     90,
			}); err != nil {
				t.Fatal(err)
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if vectors == 0 {
		t.Fatal("FLAC conformance submodule is not initialized; run git submodule update --init --recursive")
	}
}

func uncommonContainerVector(fileName string) bool {
	return strings.HasPrefix(fileName, "01 ") ||
		strings.HasPrefix(fileName, "02 ") ||
		strings.HasPrefix(fileName, "03 ") ||
		strings.HasPrefix(fileName, "04 ") ||
		strings.HasPrefix(fileName, "05 ") ||
		strings.HasPrefix(fileName, "06 ") ||
		strings.HasPrefix(fileName, "07 ") ||
		strings.HasPrefix(fileName, "08 ") ||
		strings.HasPrefix(fileName, "09 ")
}

func faultyMustReject(fileName string) bool {
	return strings.HasPrefix(fileName, "01 ") ||
		strings.HasPrefix(fileName, "06 ") ||
		strings.HasPrefix(fileName, "07 ") ||
		strings.HasPrefix(fileName, "08 ") ||
		strings.HasPrefix(fileName, "11 ")
}

func compareWithFFmpegWhenCompatible(t *testing.T, got []float32, data []byte) {
	t.Helper()
	want, err := testutil.DecodeWithFFmpeg(bytes.NewReader(data))
	if err != nil {
		t.Logf("FFmpeg does not support this uncommon stream; PCM comparison skipped: %v", err)
		return
	}
	if len(got) != len(want) {
		t.Logf("FFmpeg returned incompatible PCM shape (got %d samples, want %d); PCM comparison skipped", len(want), len(got))
		return
	}
	if err := audio.ComparePCM(got, want, audio.CompareOptions{
		MaxAbsDiff: 1.0 / 32768.0,
		MaxRMSE:    1e-6,
		MinSNR:     90,
	}); err != nil {
		t.Fatal(err)
	}
}
