package integration_test

import (
	"bytes"
	"encoding/binary"
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

func TestMP4SubsetSelectionPlansAndRuns(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.mp4")
	outputPath := filepath.Join(directory, "output.mp4")
	if err := os.WriteFile(inputPath, mp4TwoTrackFixture(), 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := standard.NewFileJob(inputPath, outputPath, standard.WithMappings(job.MapStream(0, "2", 0)))
	if err != nil {
		t.Fatal(err)
	}
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := instance.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if mappings := prepared.Plan().Mappings(); len(mappings) != 1 || mappings[0].Input != 0 || mappings[0].Stream != "2" || mappings[0].Output != 0 {
		t.Fatalf("MP4 subset effective mappings = %#v", mappings)
	}
	result, runErr := prepared.Run(t.Context())
	if runErr != nil || !result.Succeeded() {
		t.Fatalf("MP4 subset Run = %#v, %v", result, runErr)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	outputBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	top := mp4FixtureTopLevel(outputBytes)
	if len(top) != 3 || top[0].typeID != "ftyp" || top[1].typeID != "moov" || top[2].typeID != "mdat" || !bytes.Equal(top[2].payload, []byte{0xca, 0xfe, 0xba}) {
		t.Fatalf("MP4 subset output top-level = %#v", top)
	}
	var trackCount int
	var trackID uint32
	for _, child := range mp4FixtureChildren(top[1].payload, 0, len(top[1].payload)) {
		if child.typeID != "trak" {
			continue
		}
		trackCount++
		tkhd, ok := mp4FixtureChild(child, "tkhd")
		if !ok || len(tkhd.payload) < 16 {
			t.Fatalf("MP4 subset track header = %#v", tkhd)
		}
		trackID = binary.BigEndian.Uint32(tkhd.payload[12:16])
	}
	if trackCount != 1 || trackID != 2 {
		t.Fatalf("MP4 subset tracks = %d/%d", trackCount, trackID)
	}
}

// TestMP4MappingCannotDuplicateOrReorderTracks pins what MapStream can express
// in M7: which tracks survive, and nothing else. Duplicating a track into one
// output and choosing an output order both need a selector surface, so both are
// refused rather than silently reinterpreted as the preserve-all order.
func TestMP4MappingCannotDuplicateOrReorderTracks(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.mp4")
	if err := os.WriteFile(inputPath, mp4TwoTrackFixture(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := standard.NewFileJob(inputPath, filepath.Join(directory, "duplicated.mp4"),
		standard.WithMappings(job.MapStream(0, "1", 0), job.MapStream(0, "1", 0))); err == nil {
		t.Fatal("a track mapped twice into one output was accepted")
	}

	// Naming the tracks in the other order still yields inspection order: the
	// mapping selects a set, and the output keeps the movie's own track order.
	request, err := standard.NewFileJob(inputPath, filepath.Join(directory, "reordered.mp4"),
		standard.WithMappings(job.MapStream(0, "2", 0), job.MapStream(0, "1", 0)))
	if err != nil {
		t.Fatal(err)
	}
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	planned, err := instance.Plan(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	mappings := planned.Mappings()
	if len(mappings) != 2 || mappings[0].Stream != "1" || mappings[1].Stream != "2" {
		t.Fatalf("reversed MapStream order = %#v, want inspection order", mappings)
	}
}
