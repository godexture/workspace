package linear

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type (
	fixturePluginID  struct{}
	fixtureConfigID  struct{}
	fixtureSourceID  struct{}
	fixtureObserveID struct{}
	fixtureSinkID    struct{}
)

type fixtureConfig struct{}

type fixturePlan struct{ shape flow.Shape }

type fixtureOperator struct{ shape flow.Shape }

func (o fixtureOperator) Ports() flow.Shape { return o.shape.Clone() }
func (fixtureOperator) Close() error        { return nil }

type fixtureState struct {
	mu         sync.Mutex
	input      []byte
	read       bool
	block      bool
	output     []byte
	planes     [][]int16
	timestamps []timing.PTS
	events     []string
}

func (s *fixtureState) add(event string) {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
}

func (s *fixtureState) snapshot() ([]byte, [][]int16, []timing.PTS, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	planes := make([][]int16, len(s.planes))
	for index := range s.planes {
		planes[index] = append([]int16(nil), s.planes[index]...)
	}
	return append([]byte(nil), s.output...), planes, append([]timing.PTS(nil), s.timestamps...), append([]string(nil), s.events...)
}

type fixtureSource struct {
	fixtureOperator
	state   *fixtureState
	buffers *buffer.Allocator
}

func (s *fixtureSource) Read(ctx context.Context) (flow.Input[buffer.Handle], error) {
	if s.state.block {
		<-ctx.Done()
		return flow.Input[buffer.Handle]{}, context.Cause(ctx)
	}
	s.state.mu.Lock()
	if s.state.read {
		s.state.events = append(s.state.events, "eof")
		s.state.mu.Unlock()
		return flow.Input[buffer.Handle]{}, io.EOF
	}
	s.state.read = true
	value := append([]byte(nil), s.state.input...)
	s.state.events = append(s.state.events, "read")
	s.state.mu.Unlock()
	handle, err := s.buffers.FromBytes(value, 1)
	if err != nil {
		return flow.Input[buffer.Handle]{}, err
	}
	return flow.NewInput(handle, access.Bytes()), nil
}

type fixtureObserver struct {
	fixtureOperator
	state *fixtureState
}

func (o *fixtureObserver) Process(ctx context.Context, input flow.Input[audio.Frame[int16]], output flow.Emitter[audio.Frame[int16]]) error {
	frame := input.Value()
	o.state.mu.Lock()
	if o.state.planes == nil {
		o.state.planes = make([][]int16, len(frame.Planes().Layout().Planes))
	}
	for index := range o.state.planes {
		plane, err := frame.PlaneSamples(index)
		if err != nil {
			o.state.mu.Unlock()
			return err
		}
		o.state.planes[index] = append(o.state.planes[index], plane...)
	}
	if pts, ok := frame.PTS().Get(); ok {
		o.state.timestamps = append(o.state.timestamps, pts)
	}
	o.state.events = append(o.state.events, "frame")
	o.state.mu.Unlock()
	forwarded := flow.NewInput(frame.Share(), sample.S16())
	if err := output.Emit(ctx, forwarded); err != nil {
		forwarded.Drop()
		return err
	}
	input.Drop()
	return nil
}

func (o *fixtureObserver) Finalize(context.Context) error {
	o.state.add("finalize")
	return nil
}

func (o *fixtureObserver) Flush(context.Context, flow.Emitter[audio.Frame[int16]]) error {
	o.state.add("flow-flush")
	return nil
}

type fixtureSink struct {
	fixtureOperator
	state *fixtureState
}

func (s *fixtureSink) Write(_ context.Context, input flow.Input[buffer.Handle]) error {
	s.state.mu.Lock()
	s.state.output = append(s.state.output, input.Value().Bytes()...)
	s.state.events = append(s.state.events, "write")
	s.state.mu.Unlock()
	input.Drop()
	return nil
}

func (s *fixtureSink) Flush(context.Context) error {
	s.state.add("sink-flush")
	return nil
}

func TestCompositionDeclaresRealFormatParserAndCodec(t *testing.T) {
	index, err := catalog.Build(Set())
	if err != nil {
		t.Fatal(err)
	}
	if index.Len() != 5 {
		t.Fatalf("component count = %d", index.Len())
	}
	if !Binding().Valid() || !Raw().Valid() {
		t.Fatal("linear PCM declarations are incomplete")
	}
	read, readOK := format.ReadOf(componentByIdentity(t, ReaderIdentity()))
	write, writeOK := format.WriteOf(componentByIdentity(t, WriterIdentity()))
	if !readOK || !writeOK || read.Format().Identity() != Raw().Identity() || write.Format().Identity() != Raw().Identity() {
		t.Fatalf("linear PCM Format traits = read %#v/%v, write %#v/%v", read, readOK, write, writeOK)
	}
	capabilities, err := access.NewCapabilities(access.SequentialRead, access.RandomRead, access.StableSize)
	if err != nil {
		t.Fatal(err)
	}
	selection, ok := access.Select(capabilities, read.Requirements())
	if !ok || len(selection.Capabilities()) != 1 || selection.Capabilities()[0] != access.SequentialRead {
		t.Fatalf("raw Format narrow selection = %v, %v", selection.Capabilities(), ok)
	}
	writeCapabilities, err := access.NewCapabilities(access.SequentialWrite)
	if err != nil {
		t.Fatal(err)
	}
	writeSelection, ok := access.Select(writeCapabilities, write.Requirements())
	if !ok || len(writeSelection.Capabilities()) != 1 || writeSelection.Capabilities()[0] != access.SequentialWrite {
		t.Fatalf("raw Format write selection = %v, %v", writeSelection.Capabilities(), ok)
	}
	readerShape := componentByIdentity(t, ReaderIdentity()).Ports()
	parserShape := componentByIdentity(t, ParserIdentity()).Ports()
	writerShape := componentByIdentity(t, WriterIdentity()).Ports()
	if readerShape.Inputs[0].Schema().Identity() != access.Bytes().Identity() ||
		readerShape.Outputs[0].Schema().Identity() != format.Chunks().Identity() ||
		parserShape.Inputs[0].Schema().Identity() != format.Chunks().Identity() ||
		parserShape.Outputs[0].Schema().Identity() != codec.Packets().Identity() ||
		writerShape.Inputs[0].Schema().Identity() != codec.Packets().Identity() ||
		writerShape.Outputs[0].Schema().Identity() != access.Bytes().Identity() {
		t.Fatal("linear PCM components do not use canonical media schemas")
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

func TestPCMGrantAccountsFastAndRealtimeQueueDepth(t *testing.T) {
	description := sample.Description{Format: sample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: sample.Mono, Endian: sample.LittleEndian}
	input := []byte{
		0, 0, 1, 0, 2, 0, 3, 0, 4, 0,
		5, 0, 6, 0, 7, 0, 8, 0,
	}
	for _, preset := range []job.Preset{job.Fast, job.Realtime} {
		t.Run(preset.String(), func(t *testing.T) {
			fixture := compilePCMProgram(t, description)
			graph, ok := fixture.request.Graph()
			if !ok {
				t.Fatal("PCM fixture has no graph")
			}
			policy, ok := job.PolicyFor(preset)
			if !ok {
				t.Fatalf("policy %s is unavailable", preset)
			}
			request, err := job.New(nil, nil, graph, job.WithPolicy(policy))
			if err != nil {
				t.Fatal(err)
			}
			fixture.request = request
			output, _, _ := runPCMProgram(t, fixture, input)
			if !bytes.Equal(output, input) {
				t.Fatalf("round trip = %v, want %v", output, input)
			}
			prepared, err := fixture.host.Prepare(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			for _, node := range prepared.Plan().Nodes() {
				if node.ID == "encoder" && node.Resources.Memory != 4 {
					t.Fatalf("encoder per-item memory = %d, want 4", node.Resources.Memory)
				}
			}
		})
	}
}

func TestRealtimePlanFixesTraitAwareQueueBounds(t *testing.T) {
	description := sample.Description{Format: sample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: sample.Mono, Endian: sample.LittleEndian}
	fixture := compilePCMProgram(t, description)
	graph, ok := fixture.request.Graph()
	if !ok {
		t.Fatal("PCM fixture has no graph")
	}
	policy, ok := job.PolicyFor(job.Realtime)
	if !ok {
		t.Fatal("realtime policy did not expand")
	}
	request, err := job.New(nil, nil, graph, job.WithPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := fixture.host.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.Close(); err != nil {
			t.Error(err)
		}
	}()
	var sized bool
	for _, buffer := range prepared.Plan().Runtime().Buffers {
		if buffer.Limit.Bytes == int64(policy.Resources.Queue.Bytes) {
			sized = true
		}
	}
	if !sized {
		t.Fatalf("realtime runtime limits = %#v", prepared.Plan().Runtime().Buffers)
	}
}

func TestPCMHostRunCancellationSkipsSuccessfulFinalization(t *testing.T) {
	description := sample.Description{Format: sample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: sample.Mono, Endian: sample.LittleEndian}
	fixture := compilePCMProgram(t, description)
	fixture.state.block = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	result, err := fixture.host.Run(ctx, fixture.request)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) || result.Primary == nil || result.Primary.Phase != host.RunPhase {
		t.Fatalf("cancel result = %#v, err = %v", result, err)
	}
	_, _, _, events := fixture.state.snapshot()
	for _, event := range events {
		if event == "finalize" || event == "flow-flush" || event == "sink-flush" {
			t.Fatalf("canceled Run performed successful finalization: %v", events)
		}
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
	input := stream.MustDescriptor("pcm", codec.Packets().Identity(), timing.MustBase(1, 32_000), properties)
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

func TestPCMCompileKeepsMediaMeaningOffByteCarrierDescriptors(t *testing.T) {
	document, err := metadata.NewBuilder(metadata.StreamScope).Build()
	if err != nil {
		t.Fatal(err)
	}
	carrier := stream.MustDescriptor("pcm", access.Bytes().Identity(), access.CarrierTimeBase(), property.New()).WithMetadata(document)
	patch := config.NewPatch().SetText("rate", "32000").SetText("validBits", "12")

	reader := componentByIdentity(t, ReaderIdentity())
	resolvedReader, err := reader.Resolve(patch)
	if err != nil {
		t.Fatal(err)
	}
	compiledReader, err := plugin.Compile(reader, plugin.CompileContext{}, resolvedReader, flow.NewDescriptors(flow.Describe("bytes", carrier)))
	if err != nil {
		t.Fatal(err)
	}
	readerOutputs, ok := plugin.OutputsOf[stream.Descriptor](compiledReader)
	if !ok {
		t.Fatal("reader output descriptor type was erased incorrectly")
	}
	chunks, ok := readerOutputs.One("chunks")
	if !ok || chunks.ID() != carrier.ID() || chunks.Metadata().Scope() != metadata.StreamScope || chunks.TimeBase() != timing.MustBase(1, 32_000) {
		t.Fatalf("reader output descriptor = %#v", chunks)
	}
	description, err := sample.FromProperties(chunks.Properties())
	if err != nil || description != (sample.Description{Format: sample.S16Interleaved, ValidBits: 12, Rate: 32_000, Layout: sample.Mono, Endian: sample.LittleEndian}) {
		t.Fatalf("reader output properties = %#v, %v", description, err)
	}

	writer := componentByIdentity(t, WriterIdentity())
	resolvedWriter, err := writer.Resolve(patch)
	if err != nil {
		t.Fatal(err)
	}
	packets := stream.MustDescriptor(carrier.ID(), codec.Packets().Identity(), timing.MustBase(1, 32_000), chunks.Properties()).WithMetadata(document)
	compiledWriter, err := plugin.Compile(writer, plugin.CompileContext{}, resolvedWriter, flow.NewDescriptors(flow.Describe("packets", packets)))
	if err != nil {
		t.Fatal(err)
	}
	writerOutputs, ok := plugin.OutputsOf[stream.Descriptor](compiledWriter)
	if !ok {
		t.Fatal("writer output descriptor type was erased incorrectly")
	}
	bytesDescriptor, ok := writerOutputs.One("bytes")
	if !ok || bytesDescriptor.ID() != carrier.ID() || bytesDescriptor.Metadata().Scope() != metadata.StreamScope || bytesDescriptor.TimeBase() != access.CarrierTimeBase() || bytesDescriptor.Properties().Len() != 0 {
		t.Fatalf("writer carrier descriptor = %#v", bytesDescriptor)
	}
}

type pcmFixture struct {
	host    *host.Host
	request job.Job
	state   *fixtureState
}

func compilePCMProgram(t *testing.T, description sample.Description) pcmFixture {
	t.Helper()
	descriptor := stream.MustDescriptor("pcm", access.Bytes().Identity(), access.CarrierTimeBase(), property.New())
	state := &fixtureState{}
	definition := fixtureDefinition(descriptor, state)
	set := Set().Add(definition)
	instance, err := host.New(
		host.Plugins(set),
		host.PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
		host.Observe(host.ObservationBasic),
	)
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
			job.NewNode("observer", plugin.IdentityOf[fixtureObserveID](), config.NewPatch()),
			job.NewNode("encoder", EncoderIdentity(), patch),
			job.NewNode("writer", WriterIdentity(), patch),
			job.NewNode("sink", plugin.IdentityOf[fixtureSinkID](), config.NewPatch()),
		},
		[]job.Edge{
			job.Connect(job.At("source", "bytes"), job.At("reader", "bytes")),
			job.Connect(job.At("reader", "chunks"), job.At("decoder", "packets")),
			job.Connect(job.At("decoder", "frames"), job.At("observer", "in")),
			job.Connect(job.At("observer", "out"), job.At("encoder", "frames")),
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
	return pcmFixture{host: instance, request: request, state: state}
}

func assertPCMPlan(t *testing.T, compiled plan.Plan) {
	t.Helper()
	automatic := 0
	for _, node := range compiled.Nodes() {
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
		switch node.ID {
		case "reader":
			if node.Resources.Memory != 0 {
				t.Fatalf("zero-copy reader reserves unused payload memory: %#v", node)
			}
		case "decoder", "encoder":
			if node.Resources.Memory == 0 {
				t.Fatalf("allocating PCM node has no payload grant: %#v", node)
			}
		}
	}
	if automatic != 1 {
		t.Fatalf("automatic node count = %d, want identity Parser only", automatic)
	}
}

func runPCMProgram(t *testing.T, fixture pcmFixture, input []byte) ([]byte, [][]int16, []timing.PTS) {
	t.Helper()
	fixture.state.mu.Lock()
	fixture.state.input = append([]byte(nil), input...)
	fixture.state.mu.Unlock()
	prepared, err := fixture.host.Prepare(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	assertPCMPlan(t, prepared.Plan())
	result, err := prepared.Run(context.Background())
	if err != nil || !result.Succeeded() {
		t.Fatalf("PCM Run result = %#v, err = %v", result, err)
	}
	output, observed, timestamps, events := fixture.state.snapshot()
	assertEventOrder(t, events, "eof", "finalize", "flow-flush", "sink-flush")
	return output, observed, timestamps
}

func fixtureDefinition(descriptor stream.Descriptor, state *fixtureState) plugin.Definition {
	schema := config.Struct[fixtureConfigID](func() fixtureConfig { return fixtureConfig{} }).Version("1").Build()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("bytes", access.Bytes())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("bytes", access.Bytes())}, nil)
	source := plugin.NewComponent[fixtureSourceID](plugin.Descriptor{DisplayName: "PCM fixture source"}, schema, plugin.WithSpec(plugin.Spec[fixtureConfig, fixturePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[fixtureConfig](sourceShape),
		Compile: func(plugin.CompileContext, fixtureConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[fixturePlan, stream.Descriptor], error) {
			return plugin.Compiled[fixturePlan, stream.Descriptor]{
				Plan:      fixturePlan{shape: sourceShape},
				Outputs:   flow.NewDescriptors(flow.Describe("bytes", descriptor)),
				Resources: resource.Request{Memory: 64 * 1024},
			}, nil
		},
		Open: func(ctx plugin.OpenContext, _ fixturePlan) (flow.Operator, error) {
			return &fixtureSource{fixtureOperator: fixtureOperator{shape: sourceShape}, state: state, buffers: ctx.Buffers()}, nil
		},
	}), plugin.WithReader("bytes", access.Bytes()))
	observeShape := flow.NewShape([]flow.Port{flow.In("in", sample.S16())}, []flow.Port{flow.Out("out", sample.S16())})
	observer := plugin.NewComponent[fixtureObserveID](plugin.Descriptor{DisplayName: "PCM fixture observer"}, schema, plugin.WithSpec(plugin.Spec[fixtureConfig, fixturePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[fixtureConfig](observeShape),
		Compile: func(_ plugin.CompileContext, _ fixtureConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[fixturePlan, stream.Descriptor], error) {
			input, ok := inputs.One("in")
			if !ok {
				return plugin.Compiled[fixturePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("pcm.fixture-frame"))}}, nil
			}
			return plugin.Compiled[fixturePlan, stream.Descriptor]{
				Plan:         fixturePlan{shape: observeShape},
				Outputs:      flow.NewDescriptors(flow.Describe("out", input)),
				Finalization: plugin.RequiresFinalization,
			}, nil
		},
		Open: func(plugin.OpenContext, fixturePlan) (flow.Operator, error) {
			return &fixtureObserver{fixtureOperator: fixtureOperator{shape: observeShape}, state: state}, nil
		},
		Finalizes: true,
	}), plugin.WithProcessor("in", sample.S16(), "out", sample.S16()))
	sink := plugin.NewComponent[fixtureSinkID](plugin.Descriptor{DisplayName: "PCM fixture sink"}, schema, plugin.WithSpec(plugin.Spec[fixtureConfig, fixturePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[fixtureConfig](sinkShape),
		Compile: func(_ plugin.CompileContext, _ fixtureConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[fixturePlan, stream.Descriptor], error) {
			if _, ok := inputs.One("bytes"); !ok {
				return plugin.Compiled[fixturePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("bytes", plugin.ConditionNeed[stream.Descriptor]("pcm.fixture-input"))}}, nil
			}
			return plugin.Compiled[fixturePlan, stream.Descriptor]{Plan: fixturePlan{shape: sinkShape}, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
		},
		Open: func(plugin.OpenContext, fixturePlan) (flow.Operator, error) {
			return &fixtureSink{fixtureOperator: fixtureOperator{shape: sinkShape}, state: state}, nil
		},
	}), plugin.WithWriter("bytes", access.Bytes()))
	return plugin.Define[fixturePluginID](plugin.Descriptor{DisplayName: "PCM fixture", Version: "1"}, source, observer, sink)
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

func assertEventOrder(t *testing.T, values []string, expected ...string) {
	t.Helper()
	position := 0
	for _, value := range values {
		if position < len(expected) && value == expected[position] {
			position++
		}
	}
	if position != len(expected) {
		t.Fatalf("events %v do not contain %v", values, expected)
	}
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
