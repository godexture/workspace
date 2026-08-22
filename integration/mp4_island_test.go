package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/plugin/mp4"
	"github.com/godexture/godec/standard"
)

// TestMP4RemuxKeepsTrackOrderAcrossManySamples is the ordinary shape of a real
// movie, which the single-sample fixtures never reach: with more than one
// sample per track the demuxer is still emitting one track's samples while the
// next track's route already has work to hand over. Anything that decouples the
// routes from the reader delivers them interleaved, and the muxer -- which lays
// out one mdat region in arrival order -- refuses the movie.
//
// GOMAXPROCS=1 is the configuration that fails most reliably, so the regression
// runs there as well as on the default scheduler.
func TestMP4RemuxKeepsTrackOrderAcrossManySamples(t *testing.T) {
	stored := mp4ManySampleTwoTrackFixture(400)
	previous := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })

	for attempt := range 8 {
		directory := t.TempDir()
		inputPath := filepath.Join(directory, "many.mp4")
		outputPath := filepath.Join(directory, "many-out.mp4")
		if err := os.WriteFile(inputPath, stored, 0o600); err != nil {
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
		prepared, err := instance.Prepare(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		result, runErr := prepared.Run(t.Context())
		if runErr != nil || !result.Succeeded() {
			t.Fatalf("attempt %d multi-track remux Run = %#v, %v", attempt, result, runErr)
		}
		if err := prepared.Close(); err != nil {
			t.Fatal(err)
		}
		encoded, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(encoded, stored) {
			t.Fatalf("attempt %d changed a movie whose samples are already in track order", attempt)
		}
	}
}

// TestMP4MuxRequiresADirectIsland records why the muxer can rely on arrival
// order at all: it declares flow.Direct, so a topology that would feed the port
// from more than one task is rejected while planning rather than intermittently
// during a run.
func TestMP4MuxRequiresADirectIsland(t *testing.T) {
	set := standard.Set()
	var inputs []flow.Port
	for _, component := range set.Components() {
		if component.Identity() == mp4.MuxerIdentity() {
			inputs = component.Ports().Inputs
		}
	}
	if len(inputs) != 1 || inputs[0].FanIn() != flow.SerialFanIn || !inputs[0].Direct() {
		t.Fatalf("MP4 mux packet port = %#v, want a direct serial fan-in", inputs)
	}

	directory := t.TempDir()
	inputPath := filepath.Join(directory, "plan.mp4")
	if err := os.WriteFile(inputPath, mp4ManySampleTwoTrackFixture(4), 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := standard.NewFileJob(inputPath, filepath.Join(directory, "plan-out.mp4"))
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
	defer prepared.Close()
	value := prepared.Plan()

	fanIns := value.Runtime().FanIns
	if len(fanIns) != 1 || !fanIns[0].Direct || fanIns[0].Policy != flow.SerialFanIn || fanIns[0].Tolerance != 0 {
		t.Fatalf("MP4 remux fan-in projection = %#v", fanIns)
	}
	for _, buffer := range value.Runtime().Buffers {
		if buffer.ToNode == fanIns[0].Node {
			t.Fatalf("MP4 mux input projected a buffer: %#v", buffer)
		}
	}
	// The muxer is the only port that needs the guarantee, so nothing else in
	// the official composition should be paying for it.
	for _, other := range set.Components() {
		if other.Identity() == mp4.MuxerIdentity() {
			continue
		}
		for _, port := range other.Ports().Inputs {
			if port.Direct() {
				t.Fatalf("%s declares a direct port without needing one", other.Identity())
			}
		}
	}
}
