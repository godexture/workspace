package integration_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin/pcm/linear"
	"github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/standard"
)

func TestFileJobSelectsRequestedOutputFormatEndToEnd(t *testing.T) {
	payload := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	inputBytes := riffFile(
		riffChunk("fmt ", pcmFormat(2, 44_100, 16), 0),
		riffChunk("data", payload, 0),
	)
	for _, preset := range []job.Preset{job.Fast, job.Realtime} {
		for _, extension := range []string{"raw", "wav"} {
			t.Run(preset.String()+"/"+extension, func(t *testing.T) {
				directory := t.TempDir()
				inputPath := filepath.Join(directory, "input.wav")
				outputPath := filepath.Join(directory, "output."+extension)
				if err := os.WriteFile(inputPath, inputBytes, 0o600); err != nil {
					t.Fatal(err)
				}
				request, err := standard.NewFileJob(inputPath, outputPath)
				if err != nil {
					t.Fatal(err)
				}
				policy, _ := job.PolicyFor(preset)
				request, err = job.New(request.Inputs(), request.Outputs(), job.Graph{}, job.WithPolicy(policy))
				if err != nil {
					t.Fatal(err)
				}
				instance, err := host.New(
					host.Plugins(standard.Set()),
					host.PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
				)
				if err != nil {
					t.Fatal(err)
				}
				prepared, err := instance.Prepare(t.Context(), request)
				if err != nil {
					t.Fatal(err)
				}
				assertOutputFormatNode(t, prepared.Plan(), extension)
				result, runErr := prepared.Run(t.Context())
				if runErr != nil || !result.Succeeded() {
					t.Fatalf("Run = %#v, %v", result, runErr)
				}
				if err := prepared.Close(); err != nil {
					t.Fatal(err)
				}
				encoded, err := os.ReadFile(outputPath)
				if err != nil {
					t.Fatal(err)
				}
				if extension == "raw" {
					if !bytes.Equal(encoded, payload) {
						t.Fatalf("raw output = %x, want %x", encoded, payload)
					}
				} else {
					assertPCMRIFF(t, encoded, pcmFormat(2, 44_100, 16), payload)
				}
			})
		}
	}
}

// TestDefaultWAVEToWAVEPlanAvoidsCodecRoundTrip pins M6 stream copy until M7 owns it as policy.
func TestDefaultWAVEToWAVEPlanAvoidsCodecRoundTrip(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	outputPath := filepath.Join(directory, "output.wav")
	if err := os.WriteFile(inputPath, riffFile(
		riffChunk("fmt ", pcmFormat(2, 44_100, 16), 0),
		riffChunk("data", []byte{1, 0, 2, 0}, 0),
	), 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := standard.NewFileJob(inputPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	selected, err := instance.Plan(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range selected.Nodes() {
		if node.Component == linear.DecoderIdentity().String() || node.Component == linear.EncoderIdentity().String() {
			t.Fatalf("default WAVE remux selected codec round-trip node %#v", node)
		}
	}
}

func TestFileJobRequiresRawMediaConfigBeyondPathExtension(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.raw")
	outputPath := filepath.Join(directory, "output.raw")
	if err := os.WriteFile(inputPath, []byte("0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := standard.NewFileJob(inputPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	_, err = instance.Plan(t.Context(), request)
	items := host.Diagnostics(err)
	if len(items) != 1 || items[0].Code != "prepare.format-config-required" || items[0].Detail["required"] != "endian,layout,rate,validBits" {
		t.Fatalf("raw config diagnostic = %#v, %v", items, err)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed raw planning touched output: %v", statErr)
	}
}

func TestFileJobContentEvidenceOverridesExtensionHint(t *testing.T) {
	payload := []byte{1, 0, 2, 0}
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "misleading.raw")
	outputPath := filepath.Join(directory, "output.raw")
	if err := os.WriteFile(inputPath, riffFile(
		riffChunk("fmt ", pcmFormat(1, 48_000, 16), 0),
		riffChunk("data", payload, 0),
	), 0o600); err != nil {
		t.Fatal(err)
	}
	request, _ := standard.NewFileJob(inputPath, outputPath)
	instance, _ := standard.NewHost()
	result, err := instance.Run(t.Context(), request)
	if err != nil || !result.Succeeded() {
		t.Fatalf("content-first Run = %#v, %v", result, err)
	}
	encoded, err := os.ReadFile(outputPath)
	if err != nil || !bytes.Equal(encoded, payload) {
		t.Fatalf("content-first raw output = %x, %v", encoded, err)
	}
}

func TestFileJobDefaultsExtensionlessOutputToSelectedInputFormat(t *testing.T) {
	payload := []byte{1, 0, 2, 0}
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	outputPath := filepath.Join(directory, "output")
	if err := os.WriteFile(inputPath, riffFile(
		riffChunk("fmt ", pcmFormat(1, 48_000, 16), 0),
		riffChunk("data", payload, 0),
	), 0o600); err != nil {
		t.Fatal(err)
	}
	request, _ := standard.NewFileJob(inputPath, outputPath)
	instance, _ := standard.NewHost()
	prepared, err := instance.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	assertOutputFormatNode(t, prepared.Plan(), "wav")
	result, runErr := prepared.Run(t.Context())
	if runErr != nil || !result.Succeeded() {
		t.Fatalf("extensionless Run = %#v, %v", result, runErr)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	assertPCMRIFF(t, encoded, pcmFormat(1, 48_000, 16), payload)
}

func TestFileJobRejectsUnknownOutputExtensionBeforeOutputAcquire(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	outputPath := filepath.Join(directory, "output.unknown")
	if err := os.WriteFile(inputPath, riffFile(
		riffChunk("fmt ", pcmFormat(1, 48_000, 16), 0),
		riffChunk("data", []byte{1, 0}, 0),
	), 0o600); err != nil {
		t.Fatal(err)
	}
	request, _ := standard.NewFileJob(inputPath, outputPath)
	instance, _ := standard.NewHost()
	_, err := instance.Prepare(t.Context(), request)
	items := host.Diagnostics(err)
	if len(items) != 1 || items[0].Code != "prepare.format-not-found" || items[0].Detail["selector"] != "extension:.unknown" || items[0].Detail["direction"] != "write" {
		t.Fatalf("unknown output diagnostic = %#v, %v", items, err)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unknown output extension touched output: %v", statErr)
	}
}

func TestFileJobRejectsTruncatedWAVEBeforeOutputAcquire(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	outputPath := filepath.Join(directory, "output.wav")
	complete := riffFile(
		riffChunk("fmt ", pcmFormat(1, 48_000, 16), 0),
		riffChunk("data", []byte{1, 0, 2, 0}, 0),
	)
	if err := os.WriteFile(inputPath, complete[:len(complete)-2], 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := standard.NewFileJob(inputPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	_, err = instance.Prepare(t.Context(), request)
	if !errors.Is(err, wave.ErrTruncatedData) {
		t.Fatalf("Prepare error = %v", err)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("truncated input touched output: %v", statErr)
	}
	temporaries, globErr := filepath.Glob(filepath.Join(directory, ".output.wav.godec-*"))
	if globErr != nil || len(temporaries) != 0 {
		t.Fatalf("truncated input temporary outputs = %v, %v", temporaries, globErr)
	}
}

func TestStandardConvertUsesTheSameHostPathAndPreservesAtomicOutput(t *testing.T) {
	payload := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	inputBytes := riffFile(
		riffChunk("fmt ", pcmFormat(2, 44_100, 16), 0),
		riffChunk("data", payload, 0),
	)
	for _, extension := range []string{"wav", "raw"} {
		t.Run(extension, func(t *testing.T) {
			directory := t.TempDir()
			inputPath := filepath.Join(directory, "input.wav")
			outputPath := filepath.Join(directory, "output."+extension)
			if err := os.WriteFile(inputPath, inputBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := standard.Convert(t.Context(), inputPath, outputPath); err != nil {
				t.Fatal(err)
			}
			encoded, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			if extension == "raw" {
				if !bytes.Equal(encoded, payload) {
					t.Fatalf("raw output = %x, want %x", encoded, payload)
				}
				return
			}
			assertPCMRIFF(t, encoded, pcmFormat(2, 44_100, 16), payload)
		})
	}

	t.Run("same path rejected", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "audio.wav")
		if err := os.WriteFile(path, inputBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		err := standard.Convert(t.Context(), path, path)
		items := host.Diagnostics(err)
		if len(items) != 1 || items[0].Code != "file.same-path" {
			t.Fatalf("same-path diagnostic = %#v, %v", items, err)
		}
		encoded, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(encoded, inputBytes) {
			t.Fatal("same-path rejection changed the original input")
		}
	})

	t.Run("existing distinct target replaced", func(t *testing.T) {
		directory := t.TempDir()
		inputPath := filepath.Join(directory, "input.wav")
		outputPath := filepath.Join(directory, "output.wav")
		if err := os.WriteFile(inputPath, inputBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(outputPath, []byte("existing target"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := standard.Convert(t.Context(), inputPath, outputPath); err != nil {
			t.Fatal(err)
		}
		encoded, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatal(err)
		}
		assertPCMRIFF(t, encoded, pcmFormat(2, 44_100, 16), payload)
	})

	t.Run("runtime rollback", func(t *testing.T) {
		directory := t.TempDir()
		inputPath := filepath.Join(directory, "partial.wav")
		outputPath := filepath.Join(directory, "existing.wav")
		partial := riffFile(
			riffChunk("fmt ", pcmFormat(1, 48_000, 16), 0),
			riffChunk("data", []byte{1}, 0),
		)
		original := []byte("existing output remains")
		if err := os.WriteFile(inputPath, partial, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(outputPath, original, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := standard.Convert(t.Context(), inputPath, outputPath); err == nil {
			t.Fatal("partial PCM conversion unexpectedly succeeded")
		}
		got, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, original) {
			t.Fatalf("failed conversion changed output = %q", got)
		}
	})

	t.Run("raw input requires explicit media config", func(t *testing.T) {
		directory := t.TempDir()
		inputPath := filepath.Join(directory, "input.raw")
		outputPath := filepath.Join(directory, "output.wav")
		if err := os.WriteFile(inputPath, []byte("0123456789abcdef"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := standard.Convert(t.Context(), inputPath, outputPath)
		items := host.Diagnostics(err)
		if len(items) != 1 || items[0].Code != "prepare.format-config-required" || items[0].Detail["required"] != "endian,layout,rate,validBits" {
			t.Fatalf("raw one-line diagnostic = %#v, %v", items, err)
		}
		if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed raw convenience touched output: %v", statErr)
		}
	})
}

func assertOutputFormatNode(t testing.TB, value plan.Plan, extension string) {
	t.Helper()
	want := linear.WriterIdentity()
	if extension == "wav" {
		want = wave.MuxerIdentity()
	}
	for _, node := range value.Nodes() {
		if node.Component == want.String() {
			if node.Origin != plan.Automatic || node.Reason != "format.output" {
				t.Fatalf("output Format node = %#v", node)
			}
			if extension == "raw" {
				fields := make(map[string]string)
				for _, field := range node.Config.Fields() {
					fields[field.ID] = field.Value
				}
				if fields["rate"] != "44100" || fields["layout"] != "stereo" {
					t.Fatalf("raw output did not inherit inspected properties: %#v", node.Config.Fields())
				}
			}
			wantCapability := access.SequentialWrite
			if extension == "wav" {
				wantCapability = access.RandomWrite
			}
			for _, boundary := range value.Boundaries() {
				if boundary.Direction == plan.OutputBoundary && (len(boundary.Selected) != 1 || boundary.Selected[0] != wantCapability) {
					t.Fatalf("output Format capability = %#v", boundary)
				}
			}
			return
		}
	}
	t.Fatalf("output Format component %s is absent", want)
}
