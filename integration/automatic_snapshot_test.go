package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/standard"
)

func TestExtensionlessAutomaticProbeRequiresStableSnapshotIdentity(t *testing.T) {
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
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input")
	outputPath := filepath.Join(directory, "output.wav")
	if err := os.WriteFile(inputPath, original, 0o600); err != nil {
		t.Fatal(err)
	}

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
	defer func() { _ = prepared.Close() }()

	var inputBoundary plan.Boundary
	for _, boundary := range prepared.Plan().Boundaries() {
		if boundary.Direction == plan.InputBoundary {
			inputBoundary = boundary
			break
		}
	}
	if !inputBoundary.Valid() || !containsCapability(inputBoundary.Selected, access.StableSize) {
		t.Fatalf("extensionless automatic input binding = %#v, want final stable-size selection", inputBoundary)
	}

	grown := riffFile(
		riffChunk("fmt ", pcmFormat(2, 48_000, 16), 0),
		riffChunk("data", append(append([]byte(nil), payload...), payload...), 0),
	)
	if err := os.WriteFile(inputPath, grown, 0o600); err != nil {
		t.Fatal(err)
	}
	result, runErr := prepared.Run(context.Background())
	if runErr == nil || result.Succeeded() || !strings.Contains(runErr.Error(), "source content changed") {
		t.Fatalf("extensionless automatic mutation result = %#v, error %v", result, runErr)
	}
}

func containsCapability(values []access.Capability, want access.Capability) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
