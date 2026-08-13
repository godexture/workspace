package integration_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strconv"
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
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/pcm/linear"
	"github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/standard"
)

type waveDecoyPluginID struct{}
type waveDecoyParserID struct{}
type waveDecoyConfigID struct{}
type waveDecoyConfig struct{}
type waveDecoyPlan struct{ shape flow.Shape }
type waveDecoyOperator struct{ shape flow.Shape }

func (o waveDecoyOperator) Ports() flow.Shape { return o.shape.Clone() }
func (waveDecoyOperator) Close() error        { return nil }
func (waveDecoyOperator) Process(context.Context, *flow.Item[packet.Chunk], flow.Emitter[packet.Packet]) error {
	return errors.New("other-tag parser was opened")
}
func (waveDecoyOperator) Flush(context.Context, flow.Emitter[packet.Packet]) error { return nil }

func TestWAVEFileToRawPCMEndToEnd(t *testing.T) {
	for _, preset := range []job.Preset{job.Fast, job.Realtime} {
		t.Run(preset.String(), func(t *testing.T) {
			runWAVEFileToRawPCM(t, preset, false)
		})
	}
}

func TestAutomaticWAVEFileToRawPCMEndToEnd(t *testing.T) {
	for _, preset := range []job.Preset{job.Fast, job.Realtime} {
		t.Run(preset.String(), func(t *testing.T) {
			runWAVEFileToRawPCM(t, preset, true)
		})
	}
}

func runWAVEFileToRawPCM(t *testing.T, preset job.Preset, automatic bool) {
	t.Helper()
	payload := []byte{
		0x01, 0x00, 0xff, 0x7f,
		0xff, 0xff, 0x00, 0x80,
		0x34, 0x12, 0xcc, 0xed,
		0x00, 0x00, 0x01, 0x00,
	}
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	outputPath := filepath.Join(directory, "output.pcm")
	inputBytes := riffFile(
		riffChunk("fmt ", pcmFormat(2, 48_000, 16), 0),
		riffChunk("JUNK", []byte{0xff}, 0),
		riffChunk("data", payload, 0),
	)
	if err := os.WriteFile(inputPath, inputBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	instance, err := host.New(
		host.Plugins(standard.Set()),
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
	nodes := []job.Node{
		job.NewNode("decoder", linear.DecoderIdentity(), patch),
		job.NewNode("encoder", linear.EncoderIdentity(), patch),
		job.NewNode("writer", linear.WriterIdentity(), patch),
	}
	edges := []job.Edge{
		job.Connect(job.At("decoder", "frames"), job.At("encoder", "frames")),
		job.Connect(job.At("encoder", "packets"), job.At("writer", "packets")),
	}
	if !automatic {
		nodes = append([]job.Node{
			job.NewNode("demux", wave.DemuxerIdentity(), config.NewPatch()),
			job.NewNode("parser", linear.ParserIdentity(), patch),
		}, nodes...)
		edges = append([]job.Edge{
			job.Connect(job.At("demux", "chunks"), job.At("parser", "chunks")),
			job.Connect(job.At("parser", "packets"), job.At("decoder", "packets")),
		}, edges...)
	}
	requested, err := job.NewGraph(nodes, edges)
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
	if automatic {
		assertAutomaticWAVEReadPlan(t, prepared.Plan())
	} else {
		assertWAVEReadPlan(t, prepared.Plan())
	}
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
			set := standard.Set()
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
					job.NewNode("mux", wave.MuxerIdentity(), config.NewPatch()),
				},
				[]job.Edge{
					job.Connect(job.At("reader", "chunks"), job.At("parser", "chunks")),
					job.Connect(job.At("parser", "packets"), job.At("mux", "packets")),
				},
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
			assertPCMRIFF(t, encoded, pcmFormat(2, 48_000, 16), payload)
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
	beforeFormat := riffChunk("PRE!", []byte{1, 2, 3}, 0x91)
	formatChunk := riffChunk("fmt ", pcmFormat(2, 48_000, 16), 0)
	infoChunk := riffInfo(
		riffChunk("IART", []byte("First\x00"), 0),
		riffChunk("IART", []byte("Second\x00"), 0xa5),
		riffChunk("XTRA", []byte{4, 5, 6}, 0xcc),
	)
	dataChunk := riffChunk("data", payload, 0)
	afterData := riffChunk("POST", []byte{7, 8, 9}, 0xb3)
	inputBytes := riffFile(beforeFormat, formatChunk, infoChunk, dataChunk, afterData)

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
			set := standard.Set().Add(plugin.Define[waveDecoyPluginID](plugin.Descriptor{DisplayName: "other codec", Version: "1"}, decoy)).
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
					job.NewNode("demux", wave.DemuxerIdentity(), config.NewPatch()),
					job.NewNode("decoder", linear.DecoderIdentity(), patch),
					job.NewNode("encoder", linear.EncoderIdentity(), patch),
					job.NewNode("mux", wave.MuxerIdentity(), config.NewPatch()),
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
			input, err := job.InputFromReference(localFileReference(t, inputPath))
			if err != nil {
				t.Fatal(err)
			}
			output, err := job.OutputToReference(localFileReference(t, outputPath))
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
			chunks := parseRIFF(t, encoded)
			if !bytes.Equal(chunkPayload(t, chunks, "data"), payload) {
				t.Fatalf("round-trip PCM = %x, want %x", chunkPayload(t, chunks, "data"), payload)
			}
			got := preservedChunks(chunks)
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
			if len(boundary.Selected) != 2 || boundary.Selected[0] != access.RandomRead || boundary.Selected[1] != access.StableSize {
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

func assertAutomaticWAVEReadPlan(t *testing.T, value plan.Plan) {
	t.Helper()
	if len(value.Warnings()) != 0 {
		t.Fatalf("content-selected WAVE warnings = %v", value.Warnings())
	}
	usage := value.Usage()
	if usage.ProbeBytes != 12 || usage.ProbeRounds != 2 {
		t.Fatalf("WAVE probe usage = %#v", usage)
	}
	foundDemux := false
	for _, node := range value.Nodes() {
		if node.Component != wave.DemuxerIdentity().String() {
			continue
		}
		foundDemux = true
		if node.Origin != plan.Automatic || node.Reason != "format.probe" || len(node.Outputs) != 1 || node.Outputs[0].Descriptor.Schema != mediaformat.Chunks().Identity().String() {
			t.Fatalf("automatic WAVE demux = %#v", node)
		}
	}
	if !foundDemux {
		t.Fatal("automatic WAVE demux is absent from Plan")
	}
	for _, boundary := range value.Boundaries() {
		if boundary.Direction == plan.InputBoundary && (len(boundary.Selected) != 2 || boundary.Selected[0] != access.RandomRead || boundary.Selected[1] != access.StableSize) {
			t.Fatalf("automatic WAVE input selection = %#v", boundary)
		}
	}
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

type riffTestChunk struct {
	id      string
	payload []byte
	raw     []byte
}

func riffChunk(identity string, payload []byte, padding byte) []byte {
	if len(identity) != 4 {
		panic("RIFF chunk identity must contain four bytes")
	}
	result := make([]byte, 8+len(payload)+len(payload)&1)
	copy(result[:4], identity)
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(payload)))
	copy(result[8:], payload)
	if len(payload)&1 != 0 {
		result[len(result)-1] = padding
	}
	return result
}

func riffInfo(fields ...[]byte) []byte {
	payload := []byte("INFO")
	for _, field := range fields {
		payload = append(payload, field...)
	}
	return riffChunk("LIST", payload, 0)
}

func riffFile(chunks ...[]byte) []byte {
	body := []byte("WAVE")
	for _, chunk := range chunks {
		body = append(body, chunk...)
	}
	result := make([]byte, 8+len(body))
	copy(result[:4], "RIFF")
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(body)))
	copy(result[8:], body)
	return result
}

func parseRIFF(t testing.TB, value []byte) []riffTestChunk {
	t.Helper()
	if len(value) < 12 || string(value[:4]) != "RIFF" || string(value[8:12]) != "WAVE" || int(binary.LittleEndian.Uint32(value[4:8])) != len(value)-8 {
		t.Fatalf("invalid RIFF image: size=%d", len(value))
	}
	var result []riffTestChunk
	for offset := 12; offset < len(value); {
		if len(value)-offset < 8 {
			t.Fatalf("truncated RIFF chunk header at %d", offset)
		}
		size := int(binary.LittleEndian.Uint32(value[offset+4 : offset+8]))
		end := offset + 8 + size
		padded := end + size&1
		if end < offset || padded > len(value) {
			t.Fatalf("invalid RIFF chunk %q size %d at %d", value[offset:offset+4], size, offset)
		}
		result = append(result, riffTestChunk{
			id:      string(value[offset : offset+4]),
			payload: append([]byte(nil), value[offset+8:end]...),
			raw:     append([]byte(nil), value[offset:padded]...),
		})
		offset = padded
	}
	return result
}

func chunkPayload(t testing.TB, chunks []riffTestChunk, identity string) []byte {
	t.Helper()
	for _, chunk := range chunks {
		if chunk.id == identity {
			return chunk.payload
		}
	}
	t.Fatalf("RIFF chunk %q is absent", identity)
	return nil
}

func preservedChunks(chunks []riffTestChunk) [][]byte {
	result := make([][]byte, 0, len(chunks))
	for _, chunk := range chunks {
		switch chunk.id {
		case "JUNK", "fmt ", "data":
			continue
		default:
			result = append(result, chunk.raw)
		}
	}
	return result
}

func assertPCMRIFF(t testing.TB, encoded, format, payload []byte) {
	t.Helper()
	chunks := parseRIFF(t, encoded)
	if got := chunkPayload(t, chunks, "fmt "); !bytes.Equal(got, format) {
		t.Fatalf("WAVE format = %x, want %x", got, format)
	}
	if got := chunkPayload(t, chunks, "data"); !bytes.Equal(got, payload) {
		t.Fatalf("WAVE payload = %x, want %x", got, payload)
	}
}
