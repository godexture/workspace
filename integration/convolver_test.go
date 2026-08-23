package integration_test

import (
	"os"
	"path/filepath"
	"strconv"
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

// convolverGraph gives the convolver its two inputs as separate branches, each
// leaving its reader's byte port open. The impulse response is a port of its
// own rather than another edge of the signal's, which is what lets the planner
// bridge it on its own and what lets it be read first.
func convolverGraph(t *testing.T, hop int) job.Graph {
	t.Helper()
	nodes := []job.Node{
		job.NewNode("convolve", pluginaudio.ProcessorIdentity(pluginaudio.Convolver),
			config.NewPatch().SetText("blockSize", strconv.Itoa(hop)).SetText("normalize", "false")),
		job.NewNode("narrow", pluginaudio.ConverterIdentity(sample.F32, sample.S16), config.NewPatch()),
		job.NewNode("encode", linear.EncoderIdentity(sample.S16), config.NewPatch()),
		job.NewNode("mux", wave.MuxerIdentity(), config.NewPatch()),
	}
	edges := []job.Edge{
		job.Connect(job.At("convolve", "convolved"), job.At("narrow", "frames")),
		job.Connect(job.At("narrow", "converted"), job.At("encode", "frames")),
		job.Connect(job.At("encode", "packets"), job.At("mux", "packets")),
	}
	for prefix, port := range map[string]string{"signal": "in", "impulse": "ir"} {
		nodes = append(nodes,
			job.NewNode(job.NodeID(prefix+"-demux"), wave.DemuxerIdentity(), config.NewPatch()),
			job.NewNode(job.NodeID(prefix+"-parse"), linear.ParserIdentity(), config.NewPatch()),
			job.NewNode(job.NodeID(prefix+"-decode"), linear.DecoderIdentity(sample.S16), config.NewPatch()),
			job.NewNode(job.NodeID(prefix+"-widen"), pluginaudio.ConverterIdentity(sample.S16, sample.F32), config.NewPatch()),
		)
		edges = append(edges,
			job.Connect(job.At(job.NodeID(prefix+"-demux"), "chunks"), job.At(job.NodeID(prefix+"-parse"), "chunks")),
			job.Connect(job.At(job.NodeID(prefix+"-parse"), "packets"), job.At(job.NodeID(prefix+"-decode"), "packets")),
			job.Connect(job.At(job.NodeID(prefix+"-decode"), "frames"), job.At(job.NodeID(prefix+"-widen"), "frames")),
			job.Connect(job.At(job.NodeID(prefix+"-widen"), "converted"), job.At("convolve", port)),
		)
	}
	graph, err := job.NewGraph(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

// Convolving with a single full-scale sample is the identity, delayed by the
// hop the transform works in. That is the one impulse response whose result
// can be stated exactly, which is what makes it the test: everything the
// partitioning, the delay line and the overlap-save windowing do has to cancel
// out to the signal itself.
func TestConvolverWithAnImpulseReturnsTheSignal(t *testing.T) {
	const hop = 64
	directory := t.TempDir()
	signalPath := filepath.Join(directory, "signal.wav")
	impulsePath := filepath.Join(directory, "impulse.wav")
	outputPath := filepath.Join(directory, "convolved.wav")

	signal := pcmRamp(8, 0x0400)
	impulse := make([]byte, 2)
	impulse[0], impulse[1] = 0x00, 0x40 // one sample at half scale
	if err := os.WriteFile(signalPath, riffWAVE(signal, 1, 48_000, 16), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(impulsePath, riffWAVE(impulse, 1, 48_000, 16), 0o600); err != nil {
		t.Fatal(err)
	}

	inputs := make([]job.Input, 0, 2)
	for _, pair := range [][2]string{{signalPath, "signal-demux"}, {impulsePath, "impulse-demux"}} {
		reference, err := file.Reference(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		input, err := job.InputFromReference(reference)
		if err != nil {
			t.Fatal(err)
		}
		named, err := input.WithPort(job.At(job.NodeID(pair[1]), "bytes"))
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, named)
	}
	outputReference, err := file.Reference(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	output, err := job.OutputToReference(outputReference)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New(inputs, []job.Output{output}, convolverGraph(t, hop))
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
		t.Fatalf("convolver Run = %#v, %v", result, runErr)
	}
	executed := prepared.Plan()
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if !ranComponent(executed, pluginaudio.ProcessorIdentity(pluginaudio.Convolver).String()) {
		t.Fatalf("the executed Plan never ran a convolver: %#v", executed.Nodes())
	}

	samples := pcmSamples(t, mustRead(t, outputPath))
	if len(samples) != hop {
		t.Fatalf("produced %d samples, want one hop", len(samples))
	}
	// Half scale halves the signal, and the eight samples of it sit at the
	// start of the hop with silence behind them.
	for index := range hop {
		want := int16(0)
		if index < 8 {
			want = int16(0x0400 * (index + 1) / 2)
		}
		if difference := samples[index] - want; difference > 1 || difference < -1 {
			t.Fatalf("sample %d = %d, want about %d", index, samples[index], want)
		}
	}
}

// A prior input works because the branches are independent: the others simply
// wait. A graph where one node feeds both cannot wait, so it is refused during
// planning rather than run until it stalls.
func TestAPriorBranchMayNotShareANodeWithTheSignal(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	outputPath := filepath.Join(directory, "convolved.wav")
	if err := os.WriteFile(inputPath, riffWAVE(pcmRamp(8, 0x0400), 1, 48_000, 16), 0o600); err != nil {
		t.Fatal(err)
	}
	nodes := []job.Node{
		job.NewNode("demux", wave.DemuxerIdentity(), config.NewPatch()),
		job.NewNode("parse", linear.ParserIdentity(), config.NewPatch()),
		job.NewNode("decode", linear.DecoderIdentity(sample.S16), config.NewPatch()),
		job.NewNode("widen", pluginaudio.ConverterIdentity(sample.S16, sample.F32), config.NewPatch()),
		job.NewNode("also", pluginaudio.ConverterIdentity(sample.S16, sample.F32), config.NewPatch()),
		job.NewNode("convolve", pluginaudio.ProcessorIdentity(pluginaudio.Convolver),
			config.NewPatch().SetText("blockSize", "64")),
		job.NewNode("narrow", pluginaudio.ConverterIdentity(sample.F32, sample.S16), config.NewPatch()),
		job.NewNode("encode", linear.EncoderIdentity(sample.S16), config.NewPatch()),
		job.NewNode("mux", wave.MuxerIdentity(), config.NewPatch()),
	}
	edges := []job.Edge{
		job.Connect(job.At("demux", "chunks"), job.At("parse", "chunks")),
		job.Connect(job.At("parse", "packets"), job.At("decode", "packets")),
		job.Connect(job.At("decode", "frames"), job.At("widen", "frames")),
		job.Connect(job.At("decode", "frames"), job.At("also", "frames")),
		job.Connect(job.At("widen", "converted"), job.At("convolve", "in")),
		job.Connect(job.At("also", "converted"), job.At("convolve", "ir")),
		job.Connect(job.At("convolve", "convolved"), job.At("narrow", "frames")),
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
	if _, err := instance.Prepare(t.Context(), request); err == nil {
		t.Fatal("a prior branch sharing a node with the signal was accepted")
	}
}

// convolverConformancePlan is the coverage evidence for a component the typed
// runner cannot drive: it reads two ports, and the runner models one stream
// through one.
func convolverConformancePlan(t *testing.T) plan.Plan {
	t.Helper()
	directory := t.TempDir()
	signalPath := filepath.Join(directory, "signal.wav")
	impulsePath := filepath.Join(directory, "impulse.wav")
	outputPath := filepath.Join(directory, "convolved.wav")
	if err := os.WriteFile(signalPath, riffWAVE(pcmRamp(8, 0x0400), 1, 48_000, 16), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(impulsePath, riffWAVE([]byte{0x00, 0x40}, 1, 48_000, 16), 0o600); err != nil {
		t.Fatal(err)
	}
	inputs := make([]job.Input, 0, 2)
	for _, pair := range [][2]string{{signalPath, "signal-demux"}, {impulsePath, "impulse-demux"}} {
		reference, err := file.Reference(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		input, err := job.InputFromReference(reference)
		if err != nil {
			t.Fatal(err)
		}
		named, err := input.WithPort(job.At(job.NodeID(pair[1]), "bytes"))
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, named)
	}
	outputReference, err := file.Reference(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	output, err := job.OutputToReference(outputReference)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New(inputs, []job.Output{output}, convolverGraph(t, 64))
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
		t.Fatalf("convolver Run = %#v, %v", result, runErr)
	}
	executed := prepared.Plan()
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	return executed
}
