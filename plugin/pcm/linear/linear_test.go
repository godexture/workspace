package linear

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/internal/program"
	"github.com/godexture/godec/internal/solve"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type (
	fixturePluginID struct{}
	fixtureConfigID struct{}
	fixtureSourceID struct{}
	fixtureSinkID   struct{}
)

type fixtureConfig struct{}

type fixturePlan struct{ shape flow.Shape }

type fixtureOperator struct{ shape flow.Shape }

func (o fixtureOperator) Ports() flow.Shape { return o.shape.Clone() }
func (fixtureOperator) Close() error        { return nil }

func TestCompositionDeclaresRealFormatParserAndCodec(t *testing.T) {
	index, err := catalog.Build(Set())
	if err != nil {
		t.Fatal(err)
	}
	if index.Len() != 5 {
		t.Fatalf("component count = %d", index.Len())
	}
	if !Binding().Valid() || !Raw().Valid() || len(Raw().Alternatives()) != 2 {
		t.Fatal("linear PCM declarations are incomplete")
	}
	capabilities, err := access.NewCapabilities(access.SequentialRead, access.RandomRead, access.StableSize)
	if err != nil {
		t.Fatal(err)
	}
	selection, ok := access.Select(capabilities, access.NewRequirements(Raw().Alternatives()...))
	if !ok || len(selection.Capabilities()) != 1 || selection.Capabilities()[0] != access.SequentialRead {
		t.Fatalf("raw Format narrow selection = %v, %v", selection.Capabilities(), ok)
	}
	targets := Binding().Targets()
	decoder, decoderOK := targets[0].Component()
	parser, parserOK := targets[1].Component()
	if !decoderOK || !parserOK || decoder != DecoderIdentity() || parser != ParserIdentity() {
		t.Fatalf("binding targets = %v", targets)
	}
}

func TestPlannerRunsKnownPCMBytesThroughIdentityParser(t *testing.T) {
	tests := []struct {
		name        string
		description sample.Description
		input       []byte
		planes      [][]int16
	}{
		{
			name:        "little endian mono",
			description: sample.Description{Format: sample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: sample.Mono, Endian: sample.LittleEndian},
			input:       []byte{0x00, 0x80, 0xff, 0xff, 0x00, 0x00, 0x01, 0x00, 0xff, 0x7f},
			planes:      [][]int16{{-32768, -1, 0, 1, 32767}},
		},
		{
			name:        "big endian stereo",
			description: sample.Description{Format: sample.S16Interleaved, ValidBits: 16, Rate: 44_100, Layout: sample.Stereo, Endian: sample.BigEndian},
			input:       []byte{0x80, 0x00, 0x7f, 0xff, 0x00, 0x00, 0xff, 0xff, 0x7f, 0xff, 0x00, 0x01},
			planes:      [][]int16{{-32768, 0, 32767}, {32767, -1, 1}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compiled := compilePCMProgram(t, test.description)
			output, planes, timestamps := runPCMProgram(t, compiled, test.input)
			if !bytes.Equal(output, test.input) {
				t.Fatalf("round trip = %v, want %v", output, test.input)
			}
			if len(planes) != len(test.planes) {
				t.Fatalf("plane count = %d, want %d", len(planes), len(test.planes))
			}
			for index := range planes {
				if !equalSamples(planes[index], test.planes[index]) {
					t.Fatalf("plane %d = %v, want %v", index, planes[index], test.planes[index])
				}
			}
			wantTimestamps := []timing.PTS{0, 2}
			if len(test.planes[0]) == 5 {
				wantTimestamps = []timing.PTS{0, 2, 4}
			}
			if !equalPTS(timestamps, wantTimestamps) {
				t.Fatalf("timestamps = %v, want %v", timestamps, wantTimestamps)
			}
		})
	}
}

func TestPCMCompilePreservesUnknownPropertiesAcrossRepresentation(t *testing.T) {
	type foreignID struct{}
	foreign := property.Define[foreignID](property.Scalar[string]())
	description := sample.Description{Format: sample.S16Interleaved, ValidBits: 12, Rate: 32_000, Layout: sample.Mono, Endian: sample.LittleEndian}
	properties, err := description.Properties()
	if err != nil {
		t.Fatal(err)
	}
	properties, err = property.Put(properties, foreign, "preserved")
	if err != nil {
		t.Fatal(err)
	}
	input := stream.MustDescriptor("pcm", Packets().Identity(), timing.MustBase(1, 32_000), properties)
	component := componentByIdentity(t, DecoderIdentity())
	resolved, err := component.Resolve(config.NewPatch().SetText("rate", "32000").SetText("validBits", "12"))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plugin.Compile(component, plugin.CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("packets", input)))
	if err != nil {
		t.Fatal(err)
	}
	outputs, ok := plugin.OutputsOf[stream.Descriptor](compiled)
	if !ok {
		t.Fatal("decoder output descriptor type was erased incorrectly")
	}
	output, ok := outputs.One("frames")
	if !ok || output.Schema() != sample.S16().Identity() {
		t.Fatalf("decoder output = %#v", output)
	}
	decoded, err := sample.FromProperties(output.Properties())
	if err != nil || decoded != (sample.Description{Format: sample.S16Planar, ValidBits: 12, Rate: 32_000, Layout: sample.Mono, Endian: sample.NoEndian}) {
		t.Fatalf("decoded properties = %#v, %v", decoded, err)
	}
	if value, ok := foreign.Get(output.Properties()); !ok || value != "preserved" {
		t.Fatalf("foreign property = %q, %v", value, ok)
	}
}

func compilePCMProgram(t *testing.T, description sample.Description) program.Program {
	t.Helper()
	properties, err := description.Properties()
	if err != nil {
		t.Fatal(err)
	}
	descriptor := stream.MustDescriptor("pcm", Bytes().Identity(), timing.MustBase(1, int64(description.Rate)), properties)
	definition := fixtureDefinition(descriptor)
	set := Set().Add(definition)
	index, err := catalog.Build(set)
	if err != nil {
		t.Fatal(err)
	}
	patch := config.NewPatch().
		SetText("rate", strconv.Itoa(description.Rate)).
		SetText("validBits", strconv.Itoa(description.ValidBits)).
		SetText("layout", string(description.Layout)).
		SetText("endian", string(description.Endian)).
		SetText("chunkSamples", "2")
	requested, err := job.NewGraph(
		[]job.Node{
			job.NewNode("source", plugin.IdentityOf[fixtureSourceID](), config.NewPatch()),
			job.NewNode("reader", ReaderIdentity(), patch),
			job.NewNode("decoder", DecoderIdentity(), patch),
			job.NewNode("encoder", EncoderIdentity(), patch),
			job.NewNode("writer", WriterIdentity(), patch),
			job.NewNode("sink", plugin.IdentityOf[fixtureSinkID](), config.NewPatch()),
		},
		[]job.Edge{
			job.Connect(job.At("source", "bytes"), job.At("reader", "bytes")),
			job.Connect(job.At("reader", "chunks"), job.At("decoder", "packets")),
			job.Connect(job.At("decoder", "frames"), job.At("encoder", "frames")),
			job.Connect(job.At("encoder", "packets"), job.At("writer", "packets")),
			job.Connect(job.At("writer", "bytes"), job.At("sink", "bytes")),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New(nil, nil, requested)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := solve.Resolve(context.Background(), index, request, plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"})
	if err != nil {
		t.Fatal(err)
	}
	automatic := 0
	for _, node := range compiled.Plan().Nodes() {
		if node.ID == "decoder" {
			if len(node.Outputs) != 1 || node.Outputs[0].Descriptor.Schema != sample.S16().Identity().String() {
				t.Fatalf("Plan selected decoder schema = %#v", node.Outputs)
			}
		}
		if node.Origin == plan.Automatic {
			automatic++
			if node.Component != ParserIdentity().String() || node.Reason != "graph.schema-mismatch" {
				t.Fatalf("automatic node = %#v", node)
			}
		}
	}
	if automatic != 1 {
		t.Fatalf("automatic node count = %d, want identity Parser only", automatic)
	}
	decoder, ok := compiled.Lookup("decoder")
	if !ok {
		t.Fatal("compiled decoder is missing")
	}
	input, inputOK := decoder.Inputs().One("packets")
	output, outputOK := decoder.Outputs().One("frames")
	if !inputOK || !outputOK || input.Schema() != Packets().Identity() || output.Schema() != sample.S16().Identity() {
		t.Fatalf("decoder descriptor path = %#v -> %#v", input, output)
	}
	return compiled
}

func runPCMProgram(t *testing.T, compiled program.Program, input []byte) ([]byte, [][]int16, []timing.PTS) {
	t.Helper()
	ctx := context.Background()
	reader := mustOpen[flow.Processor[[]byte, packet.Chunk]](t, compiled, "reader")
	parserID := automaticID(t, compiled, ParserIdentity())
	parser := mustOpen[flow.Processor[packet.Chunk, packet.Packet]](t, compiled, parserID)
	decoder := mustOpen[flow.Processor[packet.Packet, audio.Frame[int16]]](t, compiled, "decoder")
	encoder := mustOpen[flow.Processor[audio.Frame[int16], packet.Packet]](t, compiled, "encoder")
	writer := mustOpen[flow.Processor[packet.Packet, []byte]](t, compiled, "writer")
	operators := []flow.Operator{reader.(flow.Operator), parser.(flow.Operator), decoder.(flow.Operator), encoder.(flow.Operator), writer.(flow.Operator)}
	defer func() {
		for index := len(operators) - 1; index >= 0; index-- {
			if err := operators[index].Close(); err != nil {
				t.Errorf("close operator %d: %v", index, err)
			}
		}
	}()

	chunks := &collector[packet.Chunk]{}
	if err := reader.Process(ctx, flow.NewInput(append([]byte(nil), input...), Bytes()), chunks); err != nil {
		t.Fatal(err)
	}
	var output []byte
	var observed [][]int16
	var timestamps []timing.PTS
	for _, chunk := range chunks.items {
		packets := &collector[packet.Packet]{}
		if err := parser.Process(ctx, chunk, packets); err != nil {
			t.Fatal(err)
		}
		for _, packetInput := range packets.items {
			frames := &collector[audio.Frame[int16]]{}
			if err := decoder.Process(ctx, packetInput, frames); err != nil {
				t.Fatal(err)
			}
			for _, frameInput := range frames.items {
				frame := frameInput.Value()
				if pts, ok := frame.PTS().Get(); ok {
					timestamps = append(timestamps, pts)
				}
				if observed == nil {
					observed = make([][]int16, len(frame.Planes().Layout().Planes))
				}
				for planeIndex := range observed {
					plane, err := frame.PlaneSamples(planeIndex)
					if err != nil {
						t.Fatal(err)
					}
					observed[planeIndex] = append(observed[planeIndex], plane...)
				}
				encoded := &collector[packet.Packet]{}
				if err := encoder.Process(ctx, frameInput, encoded); err != nil {
					t.Fatal(err)
				}
				for _, encodedInput := range encoded.items {
					bytesOutput := &collector[[]byte]{}
					if err := writer.Process(ctx, encodedInput, bytesOutput); err != nil {
						t.Fatal(err)
					}
					for _, value := range bytesOutput.items {
						owner := value.Take()
						output = append(output, owner.Value()...)
						owner.Release()
					}
				}
			}
		}
	}
	return output, observed, timestamps
}

func fixtureDefinition(descriptor stream.Descriptor) plugin.Definition {
	schema := config.Struct[fixtureConfigID](func() fixtureConfig { return fixtureConfig{} }).Version("1").Build()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("bytes", Bytes())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("bytes", Bytes())}, nil)
	source := plugin.NewComponent[fixtureSourceID](plugin.Descriptor{DisplayName: "PCM fixture source"}, schema, plugin.WithSpec(plugin.Spec[fixtureConfig, fixturePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[fixtureConfig](sourceShape),
		Compile: func(plugin.CompileContext, fixtureConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[fixturePlan, stream.Descriptor], error) {
			return plugin.Compiled[fixturePlan, stream.Descriptor]{Plan: fixturePlan{shape: sourceShape}, Outputs: flow.NewDescriptors(flow.Describe("bytes", descriptor))}, nil
		},
		Open: func(plugin.OpenContext, fixturePlan) (flow.Operator, error) {
			return fixtureOperator{shape: sourceShape}, nil
		},
	}))
	sink := plugin.NewComponent[fixtureSinkID](plugin.Descriptor{DisplayName: "PCM fixture sink"}, schema, plugin.WithSpec(plugin.Spec[fixtureConfig, fixturePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[fixtureConfig](sinkShape),
		Compile: func(_ plugin.CompileContext, _ fixtureConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[fixturePlan, stream.Descriptor], error) {
			if _, ok := inputs.One("bytes"); !ok {
				return plugin.Compiled[fixturePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("bytes", plugin.ConditionNeed[stream.Descriptor]("pcm.fixture-input"))}}, nil
			}
			return plugin.Compiled[fixturePlan, stream.Descriptor]{Plan: fixturePlan{shape: sinkShape}, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
		},
		Open: func(plugin.OpenContext, fixturePlan) (flow.Operator, error) {
			return fixtureOperator{shape: sinkShape}, nil
		},
	}))
	return plugin.Define[fixturePluginID](plugin.Descriptor{DisplayName: "PCM fixture", Version: "1"}, source, sink)
}

func componentByIdentity(t *testing.T, identity plugin.Identity) plugin.Component {
	t.Helper()
	for _, component := range Plugin().Components() {
		if component.Identity() == identity {
			return component
		}
	}
	t.Fatalf("component %s is missing", identity)
	return plugin.Component{}
}

type collector[T any] struct{ items []flow.Input[T] }

func (c *collector[T]) Emit(_ context.Context, value flow.Input[T]) error {
	c.items = append(c.items, value)
	return nil
}

func mustOpen[T any](t *testing.T, compiled program.Program, id job.NodeID) T {
	t.Helper()
	allocator, err := buffer.NewAllocator(64 << 20)
	if err != nil {
		t.Fatal(err)
	}
	operator, err := compiled.Open(plugin.NewOpenContext(context.Background(), plugin.OpenServices{Buffers: allocator}), id)
	if err != nil {
		t.Fatal(err)
	}
	typed, ok := operator.(T)
	if !ok {
		_ = operator.Close()
		t.Fatalf("node %s has operator %T", id, operator)
	}
	return typed
}

func automaticID(t *testing.T, compiled program.Program, identity plugin.Identity) job.NodeID {
	t.Helper()
	for _, node := range compiled.Plan().Nodes() {
		if node.Origin == plan.Automatic && node.Component == identity.String() {
			return job.NodeID(node.ID)
		}
	}
	t.Fatalf("automatic component %s is missing", identity)
	return ""
}

func equalSamples(left, right []int16) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalPTS(left, right []timing.PTS) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
