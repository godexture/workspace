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

// filterChainGraph puts a run of gains between a 16-bit file and a 16-bit
// file, and wires no converter at all. What ends up between them is the
// planner's answer to what each stage asked for, which is the thing being
// measured.
func filterChainGraph(t *testing.T, filters int) job.Graph {
	t.Helper()
	nodes := []job.Node{
		job.NewNode("demux", wave.DemuxerIdentity(), config.NewPatch()),
		job.NewNode("parse", linear.ParserIdentity(), config.NewPatch()),
		job.NewNode("decode", linear.DecoderIdentity(sample.S16), config.NewPatch()),
		job.NewNode("encode", linear.EncoderIdentity(sample.S16), config.NewPatch()),
		job.NewNode("mux", wave.MuxerIdentity(), config.NewPatch()),
	}
	edges := []job.Edge{
		job.Connect(job.At("demux", "chunks"), job.At("parse", "chunks")),
		job.Connect(job.At("parse", "packets"), job.At("decode", "packets")),
		job.Connect(job.At("encode", "packets"), job.At("mux", "packets")),
	}
	previous := job.At("decode", "frames")
	for index := range filters {
		id := job.NodeID("gain-" + strconv.Itoa(index))
		nodes = append(nodes, job.NewNode(id, pluginaudio.ProcessorIdentity(pluginaudio.Gain),
			config.NewPatch().SetText("decibels", "0")))
		edges = append(edges, job.Connect(previous, job.At(id, "frames")))
		previous = job.At(id, "filtered")
	}
	edges = append(edges, job.Connect(previous, job.At("encode", "frames")))
	graph, err := job.NewGraph(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

func runFilterChain(t *testing.T, filters int) (plan.Plan, []int16) {
	t.Helper()
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	outputPath := filepath.Join(directory, "filtered.wav")
	if err := os.WriteFile(inputPath, riffWAVE(pcmRamp(64, 0x0100), 1, 48_000, 16), 0o600); err != nil {
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
	request, err := job.New([]job.Input{input}, []job.Output{output}, filterChainGraph(t, filters))
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
		t.Fatalf("filter chain Run = %#v, %v", result, runErr)
	}
	executed := prepared.Plan()
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	return executed, pcmSamples(t, mustRead(t, outputPath))
}

// conversions counts the stages in a Plan that exist only to restate samples
// in another representation.
func conversions(executed plan.Plan) int {
	count := 0
	for _, node := range executed.Nodes() {
		for _, effect := range node.Effects {
			if effect.Detail == "audio.sample-conversion" {
				count++
				break
			}
		}
	}
	return count
}

// This is what the whole shape of the family is for. Filters state the samples
// they read and nothing about how those samples are stored, so a run of them
// converts at the two edges of the run however long it is. The old stack
// converted twice per filter, which is thirty-two conversions for a chain of
// sixteen where two would do.
func TestAFilterRegionConvertsAtItsEdgesOnly(t *testing.T) {
	for _, filters := range []int{1, 4, 16} {
		t.Run(strconv.Itoa(filters)+"-filters", func(t *testing.T) {
			executed, samples := runFilterChain(t, filters)
			if got := conversions(executed); got != 2 {
				t.Fatalf("a chain of %d filters converted %d times, want 2", filters, got)
			}
			// Every gain is at unity, so the samples that come back are the
			// ones that went in: crossing the region and coming back is exact.
			if len(samples) != 64 {
				t.Fatalf("produced %d samples, want 64", len(samples))
			}
			for index, got := range samples {
				if want := int16(0x0100 * (index + 1)); got != want {
					t.Fatalf("sample %d = %d, want %d", index, got, want)
				}
			}
		})
	}
}
