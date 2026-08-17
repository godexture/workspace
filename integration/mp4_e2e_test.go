package integration_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin/mp4"
	"github.com/godexture/godec/standard"
)

func TestMP4StandardExplicitRemuxPreservesTracksAndPayload(t *testing.T) {
	inputBytes := mp4TwoTrackFixture()
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.mp4")
	outputPath := filepath.Join(directory, "output.mp4")
	if err := os.WriteFile(inputPath, inputBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	request := newMP4RemuxJob(t, inputPath, outputPath, job.Fast)
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := instance.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	assertMP4RemuxPlan(t, prepared.Plan(), job.Fast)
	result, runErr := prepared.Run(t.Context())
	if runErr != nil || !result.Succeeded() {
		t.Fatalf("MP4 remux Run = %#v, %v", result, runErr)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	outputBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(outputBytes, inputBytes) {
		t.Fatalf("MP4 remux changed source bytes: got %d bytes, want %d", len(outputBytes), len(inputBytes))
	}
	assertMP4FixtureSemantics(t, outputBytes)
}

func TestMP4RealtimeRemuxRejectsScratchBeforeOutputAcquire(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.mp4")
	outputPath := filepath.Join(directory, "output.mp4")
	if err := os.WriteFile(inputPath, mp4TwoTrackFixture(), 0o600); err != nil {
		t.Fatal(err)
	}
	request := newMP4RemuxJob(t, inputPath, outputPath, job.Realtime)
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	_, prepareErr := instance.Prepare(t.Context(), request)
	if prepareErr == nil {
		t.Fatal("Realtime MP4 remux unexpectedly planned with zero scratch")
	}
	items := host.Diagnostics(prepareErr)
	if len(items) != 1 || items[0].Code != "solve.unsupported" || items[0].Detail["dimension"] != "scratch" {
		t.Fatalf("Realtime planning error = %v; diagnostics = %#v", prepareErr, items)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("Realtime planning failure acquired output: %v", statErr)
	}
}

func newMP4RemuxJob(t testing.TB, inputPath, outputPath string, preset job.Preset) job.Job {
	t.Helper()
	input, err := job.InputFromReference(localFileReference(t, inputPath))
	if err != nil {
		t.Fatal(err)
	}
	output, err := job.OutputToReference(localFileReference(t, outputPath))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("demux", mp4.DemuxerIdentity(), config.NewPatch()),
			job.NewNode("mux", mp4.MuxerIdentity(), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("demux", "packets"), job.At("mux", "packets"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	policy, ok := job.PolicyFor(preset)
	if !ok {
		t.Fatalf("policy %s is unavailable", preset)
	}
	request, err := job.New([]job.Input{input}, []job.Output{output}, graph, job.WithPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func assertMP4RemuxPlan(t testing.TB, value plan.Plan, preset job.Preset) {
	t.Helper()
	nodes := value.Nodes()
	var demux, mux *plan.Node
	for index := range nodes {
		node := nodes[index]
		switch node.ID {
		case "demux":
			copy := node
			demux = &copy
		case "mux":
			copy := node
			mux = &copy
		}
	}
	if demux == nil || mux == nil {
		t.Fatalf("MP4 nodes are missing from Plan: %#v", value.Nodes())
	}
	if demux.Component != mp4.DemuxerIdentity().String() || mux.Component != mp4.MuxerIdentity().String() || demux.Origin != plan.Requested || mux.Origin != plan.Requested {
		t.Fatalf("MP4 graph nodes = %#v, %#v", *demux, *mux)
	}
	if len(demux.Outputs) != 2 || len(mux.Inputs) != 2 {
		t.Fatalf("MP4 repeated descriptors = outputs %d, inputs %d", len(demux.Outputs), len(mux.Inputs))
	}
	streams := []string{"1", "2"}
	for index, descriptor := range demux.Outputs {
		if descriptor.Port != "packets" || descriptor.Descriptor.Stream != streams[index] || !descriptor.Descriptor.HasTimeline {
			t.Fatalf("MP4 demux descriptor %d = %#v", index, descriptor)
		}
		if descriptor.Descriptor.TimeBaseNumerator != 1 || (index == 0 && descriptor.Descriptor.TimeBaseDenominator != 48_000) || (index == 1 && descriptor.Descriptor.TimeBaseDenominator != 1_000) {
			t.Fatalf("MP4 demux timing descriptor %d = %#v", index, descriptor.Descriptor)
		}
		if mux.Inputs[index].Port != "packets" || mux.Inputs[index].Descriptor.Fingerprint != descriptor.Descriptor.Fingerprint {
			t.Fatalf("MP4 mux descriptor %d = %#v, want %#v", index, mux.Inputs[index], descriptor)
		}
	}
	foundSerial := false
	for _, fanIn := range value.Runtime().FanIns {
		if fanIn.Node == "mux" && fanIn.Port == "packets" {
			if fanIn.Policy != flow.SerialFanIn {
				t.Fatalf("MP4 mux fan-in = %#v", fanIn)
			}
			foundSerial = true
		}
	}
	if !foundSerial {
		t.Fatalf("MP4 mux SerialFanIn is absent: %#v", value.Runtime().FanIns)
	}
	if mux.Scratch != 16 || value.Scratch().Reserved != 16 || value.Scratch().Limit != mustMP4Policy(t, preset).Resources.ScratchMaxBytes {
		t.Fatalf("MP4 scratch = %#v, mux = %d", value.Scratch(), mux.Scratch)
	}
	foundInput, foundOutput := false, false
	for _, boundary := range value.Boundaries() {
		switch boundary.Direction {
		case plan.InputBoundary:
			foundInput = true
			if len(boundary.Selected) != 2 || boundary.Selected[0] != access.RandomRead || boundary.Selected[1] != access.StableSize {
				t.Fatalf("MP4 input boundary = %#v", boundary)
			}
		case plan.OutputBoundary:
			foundOutput = true
			if len(boundary.Selected) != 1 || boundary.Selected[0] != access.RandomWrite {
				t.Fatalf("MP4 output boundary = %#v", boundary)
			}
		}
	}
	if !foundInput || !foundOutput {
		t.Fatalf("MP4 IO boundaries = input %v, output %v", foundInput, foundOutput)
	}
}

func mustMP4Policy(t testing.TB, preset job.Preset) job.Policy {
	t.Helper()
	value, ok := job.PolicyFor(preset)
	if !ok {
		t.Fatalf("policy %s is unavailable", preset)
	}
	return value
}
