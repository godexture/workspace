package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/standard"
)

// A local file session advertises StableSize, so Probe, Inspect, and the run
// must all describe the same bytes. Every mutation another handle can make
// between Prepare and Run is either detected and refused, or provably invisible
// to the session that already opened the file.
func TestLocalFileMutationBetweenPreparationAndRun(t *testing.T) {
	payload := []byte{
		0x01, 0x00, 0xff, 0x7f,
		0xff, 0xff, 0x00, 0x80,
		0x34, 0x12, 0xcc, 0xed,
		0x00, 0x00, 0x01, 0x00,
	}
	original := riffFile(
		riffChunk("fmt ", pcmFormat(2, 48_000, 16), 0),
		riffChunk("data", payload, 0),
	)
	grown := riffFile(
		riffChunk("fmt ", pcmFormat(2, 48_000, 16), 0),
		riffChunk("data", append(append([]byte(nil), payload...), payload...), 0),
	)
	overwritten := riffFile(
		riffChunk("fmt ", pcmFormat(2, 48_000, 16), 0),
		riffChunk("data", []byte{
			0x02, 0x00, 0xfe, 0x7f,
			0xfe, 0xff, 0x01, 0x80,
			0x35, 0x12, 0xcb, 0xed,
			0x01, 0x00, 0x02, 0x00,
		}, 0),
	)

	for _, test := range []struct {
		name    string
		mutate  func(t *testing.T, path string)
		refused bool
	}{
		{
			name:    "truncate",
			mutate:  func(t *testing.T, path string) { writeFile(t, path, original[:len(original)/2]) },
			refused: true,
		},
		{
			name:    "grow",
			mutate:  func(t *testing.T, path string) { writeFile(t, path, grown) },
			refused: true,
		},
		{
			name:    "same-size-overwrite",
			mutate:  func(t *testing.T, path string) { writeFile(t, path, overwritten) },
			refused: true,
		},
		{
			// The session holds the file it opened, not the path. Replacing
			// what the path points at leaves the acquired content intact, so
			// the run must succeed on the bytes it planned against.
			name:    "path-replacement",
			mutate:  replacePath(original, overwritten),
			refused: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			inputPath := filepath.Join(directory, "input.wav")
			outputPath := filepath.Join(directory, "output.wav")
			writeFile(t, inputPath, original)

			instance, err := host.New(
				host.Plugins(standard.Set()),
				host.PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
			)
			if err != nil {
				t.Fatal(err)
			}
			input, err := job.InputFromReference(localFileReference(t, inputPath))
			if err != nil {
				t.Fatal(err)
			}
			output, err := job.OutputToReference(localFileReference(t, outputPath))
			if err != nil {
				t.Fatal(err)
			}
			request, err := job.New([]job.Input{input}, []job.Output{output}, job.Graph{})
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := instance.Prepare(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			// A refused run reports its failure again through Close, which is
			// the contract; only an unchanged source must close cleanly.
			defer func() {
				if err := prepared.Close(); err != nil && !test.refused {
					t.Error(err)
				}
			}()

			test.mutate(t, inputPath)

			result, runErr := prepared.Run(context.Background())
			if !test.refused {
				if runErr != nil || !result.Succeeded() {
					t.Fatalf("run over an unchanged session failed: %v", runErr)
				}
				return
			}
			if runErr == nil || result.Succeeded() {
				t.Fatalf("run succeeded after the source changed: %#v", result)
			}
			if !strings.Contains(runErr.Error(), "source content changed") {
				t.Fatalf("run error = %v, want a source content change", runErr)
			}
		})
	}
}

func writeFile(t *testing.T, path string, value []byte) {
	t.Helper()
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatal(err)
	}
}

// replacePath swaps the file behind the path for a different one. Windows
// refuses to rename over a path whose file is still open, which is exactly the
// state this test is in, so the case reports that instead of failing.
func replacePath(original, replacement []byte) func(*testing.T, string) {
	return func(t *testing.T, path string) {
		t.Helper()
		staged := path + ".staged"
		writeFile(t, staged, replacement)
		if err := os.Rename(staged, path); err != nil {
			if !errors.Is(err, os.ErrPermission) && !strings.Contains(err.Error(), "being used by another process") {
				t.Fatal(err)
			}
			t.Skipf("this platform does not allow replacing an open file: %v", err)
		}
	}
}
