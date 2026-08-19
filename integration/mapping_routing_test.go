package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/job"
	"github.com/godexture/godec/standard"
)

func TestMP4PlanProjectsPreserveAllMappingsInInspectionOrder(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.mp4")
	outputPath := filepath.Join(directory, "output.mp4")
	if err := os.WriteFile(inputPath, mp4TwoTrackFixture(), 0o600); err != nil {
		t.Fatal(err)
	}
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	planned, err := instance.Plan(t.Context(), newMP4RemuxJob(t, inputPath, outputPath, job.Fast))
	if err != nil {
		t.Fatal(err)
	}
	mappings := planned.Mappings()
	if len(mappings) != 2 || mappings[0].Input != 0 || mappings[0].Stream != "1" || mappings[0].Output != 0 || mappings[1].Input != 0 || mappings[1].Stream != "2" || mappings[1].Output != 0 {
		t.Fatalf("MP4 effective mappings = %#v", mappings)
	}
}
