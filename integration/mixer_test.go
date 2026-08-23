package integration_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/plan"
	pluginaudio "github.com/godexture/godec/plugin/audio"
	"github.com/godexture/godec/plugin/file"
	"github.com/godexture/godec/plugin/pcm/linear"
	"github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/standard"
)

// A mixer is the one processor whose whole point is more than one input, so it
// is proved by a graph that gives it two. They come from one file here rather
// than two: a job with two boundaries of its own is what a surface resolves,
// and no surface does that yet.
//
// Each branch is wired out to the samples the mixer reads, because a Many
// input port carries more than one edge and the planner bridges an edge only
// where a port has exactly one of them. What the inputs have in common is
// settled by the mixer at Compile rather than by something inserted between.
func TestMixerAddsItsInputsAtTheLevelsItWasGiven(t *testing.T) {
	_, samples := mixerRun(t)
	if len(samples) != 8 {
		t.Fatalf("mixed %d samples, want 8", len(samples))
	}
	for index, got := range samples {
		// The same ramp arrives twice, at one and at a half.
		want := int16(float32(int16(0x0800*(index+1))) * 1.5)
		if got != want {
			t.Fatalf("sample %d = %d, want %d (got %v)", index, got, want, samples)
		}
	}
}

// mixerConformancePlan is the coverage evidence for a component the typed
// runner cannot drive: it takes more than one input, and the runner models one
// stream through one port.
func mixerConformancePlan(t *testing.T) plan.Plan {
	t.Helper()
	executed, _ := mixerRun(t)
	return executed
}

func mixerRun(t *testing.T) (plan.Plan, []int16) {
	t.Helper()
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	outputPath := filepath.Join(directory, "mixed.wav")
	if err := os.WriteFile(inputPath, riffWAVE(pcmRamp(8, 0x0800), 1, 48_000, 16), 0o600); err != nil {
		t.Fatal(err)
	}

	nodes := []job.Node{
		job.NewNode("demux", wave.DemuxerIdentity(), config.NewPatch()),
		job.NewNode("parse", linear.ParserIdentity(), config.NewPatch()),
		job.NewNode("decode", linear.DecoderIdentity(sample.S16), config.NewPatch()),
		job.NewNode("first", pluginaudio.ConverterIdentity(sample.S16, sample.F32), config.NewPatch()),
		job.NewNode("second", pluginaudio.ConverterIdentity(sample.S16, sample.F32), config.NewPatch()),
		job.NewNode("mix", pluginaudio.ProcessorIdentity(pluginaudio.Mixer),
			config.NewPatch().SetText("weights", `[1,0.5]`)),
		job.NewNode("narrow", pluginaudio.ConverterIdentity(sample.F32, sample.S16), config.NewPatch()),
		job.NewNode("encode", linear.EncoderIdentity(sample.S16), config.NewPatch()),
		job.NewNode("mux", wave.MuxerIdentity(), config.NewPatch()),
	}
	edges := []job.Edge{
		job.Connect(job.At("demux", "chunks"), job.At("parse", "chunks")),
		job.Connect(job.At("parse", "packets"), job.At("decode", "packets")),
		job.Connect(job.At("decode", "frames"), job.At("first", "frames")),
		job.Connect(job.At("decode", "frames"), job.At("second", "frames")),
		job.Connect(job.At("first", "converted"), job.At("mix", "inputs")),
		job.Connect(job.At("second", "converted"), job.At("mix", "inputs")),
		job.Connect(job.At("mix", "mixed"), job.At("narrow", "frames")),
		job.Connect(job.At("narrow", "converted"), job.At("encode", "frames")),
		job.Connect(job.At("encode", "packets"), job.At("mux", "packets")),
	}
	graph, err := job.NewGraph(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	inputReference, err := file.Reference(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	input, err := job.InputFromReference(inputReference)
	if err != nil {
		t.Fatal(err)
	}
	outputReference, err := file.Reference(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	output, err := job.OutputToReference(outputReference)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New([]job.Input{input}, []job.Output{output}, graph)
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
		t.Fatalf("mixer Run = %#v, %v", result, runErr)
	}
	executed := prepared.Plan()
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if !ranComponent(executed, pluginaudio.ProcessorIdentity(pluginaudio.Mixer).String()) {
		t.Fatalf("the executed Plan never ran a mixer: %#v", executed.Nodes())
	}

	return executed, pcmSamples(t, mustRead(t, outputPath))
}

func ranComponent(executed plan.Plan, identity string) bool {
	for _, node := range executed.Nodes() {
		if node.Component == identity {
			return true
		}
	}
	return false
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	produced, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return produced
}

func pcmRamp(samples, step int) []byte {
	payload := make([]byte, samples*2)
	for index := range samples {
		binary.LittleEndian.PutUint16(payload[index*2:], uint16(int16(step*(index+1))))
	}
	return payload
}

func pcmSamples(t *testing.T, produced []byte) []int16 {
	t.Helper()
	offset := len(produced) - 1
	for offset >= 4 && string(produced[offset-3:offset+1]) != "data" {
		offset--
	}
	if offset < 4 {
		t.Fatal("the produced file has no data chunk")
	}
	size := binary.LittleEndian.Uint32(produced[offset+1:])
	payload := produced[offset+5 : offset+5+int(size)]
	result := make([]int16, len(payload)/2)
	for index := range result {
		result[index] = int16(binary.LittleEndian.Uint16(payload[index*2:]))
	}
	return result
}
