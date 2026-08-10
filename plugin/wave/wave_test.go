package wave

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	mediatag "github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
	fileplugin "github.com/godexture/godec/plugin/file"
	"github.com/godexture/godec/plugin/pcm/linear"
)

type waveDecoyPluginID struct{}
type waveDecoyParserID struct{}
type waveDecoyConfigID struct{}
type waveDecoyConfig struct{}
type waveDecoyPlan struct{ shape flow.Shape }
type waveDecoyOperator struct{ shape flow.Shape }

func (o waveDecoyOperator) Ports() flow.Shape { return o.shape.Clone() }
func (waveDecoyOperator) Close() error        { return nil }
func (waveDecoyOperator) Process(context.Context, flow.Input[packet.Chunk], flow.Emitter[packet.Packet]) error {
	return errors.New("other-tag parser was opened")
}
func (waveDecoyOperator) Flush(context.Context, flow.Emitter[packet.Packet]) error { return nil }

func TestWAVEFileToRawPCMEndToEnd(t *testing.T) {
	payload := []byte{
		0x01, 0x00, 0xff, 0x7f,
		0xff, 0xff, 0x00, 0x80,
		0x34, 0x12, 0xcc, 0xed,
		0x00, 0x00, 0x01, 0x00,
	}
	for _, preset := range []job.Preset{job.Fast, job.Realtime} {
		t.Run(preset.String(), func(t *testing.T) {
			directory := t.TempDir()
			inputPath := filepath.Join(directory, "input.wav")
			outputPath := filepath.Join(directory, "output.pcm")
			if err := os.WriteFile(inputPath, testWAVE(payload, 2, 48_000, testChunk{id: "JUNK", payload: []byte{0xff}}), 0o600); err != nil {
				t.Fatal(err)
			}
			set := waveTestSet()
			instance, err := host.New(
				host.Plugins(set),
				host.PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
			)
			if err != nil {
				t.Fatal(err)
			}
			patch := config.NewPatch().
				SetText("rate", strconv.Itoa(48_000)).
				SetText("validBits", "16").
				SetText("layout", "stereo").
				SetText("endian", "little").
				SetText("chunkSamples", "2")
			requested, err := job.NewGraph(
				[]job.Node{
					job.NewNode("demux", DemuxerIdentity(), config.NewPatch()),
					job.NewNode("parser", linear.ParserIdentity(), patch),
					job.NewNode("decoder", linear.DecoderIdentity(), patch),
					job.NewNode("encoder", linear.EncoderIdentity(), patch),
					job.NewNode("writer", linear.WriterIdentity(), patch),
				},
				[]job.Edge{
					job.Connect(job.At("demux", "chunks"), job.At("parser", "chunks")),
					job.Connect(job.At("parser", "packets"), job.At("decoder", "packets")),
					job.Connect(job.At("decoder", "frames"), job.At("encoder", "frames")),
					job.Connect(job.At("encoder", "packets"), job.At("writer", "packets")),
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			input, err := job.InputFromReference(fileReference(t, inputPath))
			if err != nil {
				t.Fatal(err)
			}
			output, err := job.OutputToReference(fileReference(t, outputPath))
			if err != nil {
				t.Fatal(err)
			}
			policy, ok := job.PolicyFor(preset)
			if !ok {
				t.Fatalf("policy %s is unavailable", preset)
			}
			request, err := job.New([]job.Input{input}, []job.Output{output}, requested, job.WithPolicy(policy))
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := instance.Prepare(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := prepared.Close(); err != nil {
					t.Error(err)
				}
			}()
			assertWAVEReadPlan(t, prepared.Plan())
			result, err := prepared.Run(context.Background())
			if err != nil || !result.Succeeded() {
				t.Fatalf("Run result = %#v, error %v", result, err)
			}
			decoded, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(decoded, payload) {
				t.Fatalf("raw PCM = %v, want %v", decoded, payload)
			}
		})
	}
}

func TestRawPCMToWAVEFileEndToEnd(t *testing.T) {
	payload := []byte{
		0x01, 0x00, 0xff, 0x7f,
		0xff, 0xff, 0x00, 0x80,
		0x34, 0x12, 0xcc, 0xed,
		0x00, 0x00, 0x01, 0x00,
	}
	for _, preset := range []job.Preset{job.Fast, job.Realtime} {
		t.Run(preset.String(), func(t *testing.T) {
			directory := t.TempDir()
			inputPath := filepath.Join(directory, "input.pcm")
			outputPath := filepath.Join(directory, "output.wav")
			if err := os.WriteFile(inputPath, payload, 0o600); err != nil {
				t.Fatal(err)
			}
			set := waveTestSet()
			instance, err := host.New(
				host.Plugins(set),
				host.PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
			)
			if err != nil {
				t.Fatal(err)
			}
			patch := config.NewPatch().
				SetText("rate", strconv.Itoa(48_000)).
				SetText("validBits", "16").
				SetText("layout", "stereo").
				SetText("endian", "little").
				SetText("chunkSamples", "2")
			requested, err := job.NewGraph(
				[]job.Node{
					job.NewNode("reader", linear.ReaderIdentity(), patch),
					job.NewNode("parser", linear.ParserIdentity(), patch),
					job.NewNode("mux", MuxerIdentity(), config.NewPatch()),
				},
				[]job.Edge{
					job.Connect(job.At("reader", "chunks"), job.At("parser", "chunks")),
					job.Connect(job.At("parser", "packets"), job.At("mux", "packets")),
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			input, err := job.InputFromReference(fileReference(t, inputPath))
			if err != nil {
				t.Fatal(err)
			}
			output, err := job.OutputToReference(fileReference(t, outputPath))
			if err != nil {
				t.Fatal(err)
			}
			policy, ok := job.PolicyFor(preset)
			if !ok {
				t.Fatalf("policy %s is unavailable", preset)
			}
			request, err := job.New([]job.Input{input}, []job.Output{output}, requested, job.WithPolicy(policy))
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := instance.Prepare(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			assertWAVEWritePlan(t, prepared.Plan())
			result, err := prepared.Run(context.Background())
			if err != nil || !result.Succeeded() {
				t.Fatalf("Run result = %#v, error %v", result, err)
			}
			encoded, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			inspected, err := inspectHeader(context.Background(), memoryRandom(encoded))
			if err != nil {
				t.Fatal(err)
			}
			start := int(inspected.dataOffset)
			end := start + int(inspected.dataSize)
			want := sample.Description{Format: sample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: sample.Stereo, Endian: sample.LittleEndian}
			if inspected.rf64 || inspected.description != want || !bytes.Equal(encoded[start:end], payload) {
				t.Fatalf("WAVE output = header %#v, payload %v", inspected, encoded[start:end])
			}
		})
	}
}

func TestWAVEMetadataRoundTripUsesTagBoundParser(t *testing.T) {
	payload := []byte{
		0x01, 0x00, 0xff, 0x7f,
		0xff, 0xff, 0x00, 0x80,
		0x34, 0x12, 0xcc, 0xed,
		0x00, 0x00, 0x01, 0x00,
	}
	beforeFormat := waveTestChunk(t, "PRE!", []byte{1, 2, 3}, 0x91)
	formatChunk := waveTestChunk(t, tagFMT, pcmFormat(2, 48_000, 16), 0)
	infoChunk := infoTestList(t,
		infoTestChunk(t, "IART", []byte("First\x00"), 0),
		infoTestChunk(t, "IART", []byte("Second\x00"), 0xa5),
		infoTestChunk(t, "XTRA", []byte{4, 5, 6}, 0xcc),
	)
	dataChunk := waveTestChunk(t, tagDATA, payload, 0)
	afterData := waveTestChunk(t, "POST", []byte{7, 8, 9}, 0xb3)
	inputBytes := waveTestRIFF(t, beforeFormat, formatChunk, infoChunk, dataChunk, afterData)

	for _, preset := range []job.Preset{job.Fast, job.Realtime} {
		t.Run(preset.String(), func(t *testing.T) {
			directory := t.TempDir()
			inputPath := filepath.Join(directory, "input.wav")
			outputPath := filepath.Join(directory, "output.wav")
			if err := os.WriteFile(inputPath, inputBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			var decoyCompiles atomic.Int32
			decoy := waveDecoyParser(&decoyCompiles)
			set := waveTestSet(plugin.Define[waveDecoyPluginID](plugin.Descriptor{DisplayName: "other codec", Version: "1"}, decoy)).
				AddDeclaration(codec.Bind(mediaformat.NewTag("fixture", "other"), codec.New(linear.DecoderIdentity()), codec.NewParser(decoy.Identity())))
			instance, err := host.New(
				host.Plugins(set),
				host.PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
			)
			if err != nil {
				t.Fatal(err)
			}
			patch := config.NewPatch().
				SetText("rate", strconv.Itoa(48_000)).
				SetText("validBits", "16").
				SetText("layout", "stereo").
				SetText("endian", "little").
				SetText("chunkSamples", "2")
			requested, err := job.NewGraph(
				[]job.Node{
					job.NewNode("demux", DemuxerIdentity(), config.NewPatch()),
					job.NewNode("decoder", linear.DecoderIdentity(), patch),
					job.NewNode("encoder", linear.EncoderIdentity(), patch),
					job.NewNode("mux", MuxerIdentity(), config.NewPatch()),
				},
				[]job.Edge{
					job.Connect(job.At("demux", "chunks"), job.At("decoder", "packets")),
					job.Connect(job.At("decoder", "frames"), job.At("encoder", "frames")),
					job.Connect(job.At("encoder", "packets"), job.At("mux", "packets")),
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			input, err := job.InputFromReference(fileReference(t, inputPath))
			if err != nil {
				t.Fatal(err)
			}
			output, err := job.OutputToReference(fileReference(t, outputPath))
			if err != nil {
				t.Fatal(err)
			}
			policy, ok := job.PolicyFor(preset)
			if !ok {
				t.Fatalf("policy %s is unavailable", preset)
			}
			request, err := job.New([]job.Input{input}, []job.Output{output}, requested, job.WithPolicy(policy))
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := instance.Prepare(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			assertTagBoundParser(t, prepared.Plan(), decoy.Identity())
			result, err := prepared.Run(context.Background())
			if err != nil || !result.Succeeded() {
				t.Fatalf("Run result = %#v, error %v", result, err)
			}
			if err := prepared.Close(); err != nil {
				t.Fatal(err)
			}
			if decoyCompiles.Load() != 0 {
				t.Fatalf("other-tag parser compiled %d times", decoyCompiles.Load())
			}
			encoded, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			inspected, err := inspectHeaderWithMetadata(t.Context(), memoryRandom(encoded), infoTestResolver(t))
			if err != nil {
				t.Fatal(err)
			}
			start := int(inspected.dataOffset)
			end := start + int(inspected.dataSize)
			if !bytes.Equal(encoded[start:end], payload) {
				t.Fatalf("round-trip PCM = %x, want %x", encoded[start:end], payload)
			}
			got := restoredWaveChunks(inspected.metadata)
			want := [][]byte{beforeFormat, infoChunk, afterData}
			if len(got) != len(want) {
				t.Fatalf("round-trip metadata chunks = %x", got)
			}
			for index := range want {
				if !bytes.Equal(got[index], want[index]) {
					t.Fatalf("round-trip metadata chunk %d = %x, want %x", index, got[index], want[index])
				}
			}
		})
	}
}

func assertWAVEReadPlan(t *testing.T, value plan.Plan) {
	t.Helper()
	for _, boundary := range value.Boundaries() {
		if boundary.Direction == plan.InputBoundary {
			if len(boundary.Selected) != 1 || boundary.Selected[0] != access.RandomRead {
				t.Fatalf("WAVE input selection = %#v", boundary)
			}
		}
	}
	for _, node := range value.Nodes() {
		if node.ID != "demux" {
			continue
		}
		if len(node.Outputs) != 1 || node.Outputs[0].Descriptor.Schema != mediaformat.Chunks().Identity().String() || node.Outputs[0].Descriptor.TimeBaseNumerator != 1 || node.Outputs[0].Descriptor.TimeBaseDenominator != 48_000 {
			t.Fatalf("WAVE demux descriptor = %#v", node.Outputs)
		}
		return
	}
	t.Fatal("WAVE demux node is absent from Plan")
}

func assertWAVEWritePlan(t *testing.T, value plan.Plan) {
	t.Helper()
	for _, boundary := range value.Boundaries() {
		if boundary.Direction == plan.OutputBoundary {
			if len(boundary.Selected) != 1 || boundary.Selected[0] != access.RandomWrite {
				t.Fatalf("WAVE output selection = %#v", boundary)
			}
			return
		}
	}
	t.Fatal("WAVE output boundary is absent from Plan")
}

func assertTagBoundParser(t testing.TB, value plan.Plan, decoy plugin.Identity) {
	t.Helper()
	found := false
	for _, node := range value.Nodes() {
		if node.Component == decoy.String() {
			t.Fatal("other-tag parser entered the Plan")
		}
		if node.Component == linear.ParserIdentity().String() && node.Origin == plan.Automatic {
			found = true
		}
	}
	if !found {
		t.Fatal("tag-bound linear parser was not inserted automatically")
	}
}

func waveTestSet(extras ...plugin.Definition) plugin.Set {
	result := linear.Set().Add(fileplugin.Plugin()).Add(Plugin())
	for _, extra := range extras {
		result = result.Add(extra)
	}
	result = result.
		AddDeclaration(InfoBinding()).
		AddDeclaration(codec.Bind(PCMTag(), codec.New(linear.DecoderIdentity()), codec.NewParser(linear.ParserIdentity())))
	for _, declaration := range mediatag.Declarations() {
		result = result.AddDeclaration(declaration)
	}
	return result
}

func waveDecoyParser(compiles *atomic.Int32) plugin.Component {
	shape := flow.NewShape(
		[]flow.Port{flow.In("chunks", mediaformat.Chunks())},
		[]flow.Port{flow.Out("packets", codec.Packets())},
	)
	schema := config.Struct[waveDecoyConfigID](func() waveDecoyConfig { return waveDecoyConfig{} }).Version("1").Build()
	spec := plugin.Spec[waveDecoyConfig, waveDecoyPlan, stream.Descriptor]{
		Shape: plugin.StaticShape[waveDecoyConfig](shape),
		Compile: func(_ plugin.CompileContext, _ waveDecoyConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[waveDecoyPlan, stream.Descriptor], error) {
			if compiles != nil {
				compiles.Add(1)
			}
			input, ok := inputs.One("chunks")
			if !ok {
				return plugin.Compiled[waveDecoyPlan, stream.Descriptor]{
					Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("chunks", plugin.ConditionNeed[stream.Descriptor]("decoy.input"))},
				}, nil
			}
			output, err := stream.NewDescriptor(input.ID(), codec.Packets().Identity(), input.TimeBase(), input.Properties())
			if err != nil {
				return plugin.Compiled[waveDecoyPlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[waveDecoyPlan, stream.Descriptor]{
				Plan:    waveDecoyPlan{shape: shape.Clone()},
				Outputs: flow.NewDescriptors(flow.Describe("packets", output.WithMetadata(input.Metadata()))),
				Effects: []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "decoy-parse"}},
			}, nil
		},
		Open: func(_ plugin.OpenContext, plan waveDecoyPlan) (flow.Operator, error) {
			return waveDecoyOperator{shape: plan.shape}, nil
		},
	}
	return plugin.NewComponent[waveDecoyParserID](
		plugin.Descriptor{DisplayName: "other-tag parser"},
		schema,
		plugin.WithSpec(spec),
		plugin.WithProcessor("chunks", mediaformat.Chunks(), "packets", codec.Packets()),
	)
}

func fileReference(t *testing.T, path string) access.Reference {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	value := filepath.ToSlash(absolute)
	if runtime.GOOS == "windows" && !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	reference, err := access.Parse((&url.URL{Scheme: "file", Path: value}).String())
	if err != nil {
		t.Fatal(err)
	}
	return reference
}
