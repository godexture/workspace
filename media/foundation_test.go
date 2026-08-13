package media_test

// The walking skeleton is the permanent end-to-end regression harness for
// later milestones. It carries both typed items and stream descriptors so new
// control-plane contracts have to work through the same path as data-plane
// ownership and timing.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/internal/program"
	"github.com/godexture/godec/internal/solve"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type skeletonPluginID struct{}
type skeletonSourceID struct{}
type skeletonDemuxerID struct{}
type skeletonParserID struct{}
type skeletonCodecID struct{}
type skeletonEncoderID struct{}
type skeletonMuxerID struct{}
type skeletonSinkID struct{}
type skeletonMetadataEncodingID struct{}
type skeletonMetadataEventID struct{}
type skeletonMissingMetadataID struct{}
type skeletonFormatID struct{}
type skeletonPayloadCarrierID struct{}
type skeletonMetadataCarrierID struct{}
type skeletonSampleRateID struct{}
type skeletonBytesID struct{}
type skeletonChunkID struct{}
type skeletonPacketID struct{}
type skeletonFrameID struct{}
type skeletonConfig struct{ Value int }

var (
	skeletonBytesSchema = schema.Define[skeletonBytesID, []byte](schema.Traits[[]byte]{
		Fork: func(value []byte) []byte { return append([]byte(nil), value...) },
	})
	skeletonChunkSchema = schema.Define[skeletonChunkID, packet.Chunk](schema.Traits[packet.Chunk]{
		Fork: func(value packet.Chunk) packet.Chunk { return value.Share() },
		Drop: func(value packet.Chunk) { value.Release() },
	})
	skeletonPacketSchema = schema.Define[skeletonPacketID, packet.Packet](schema.Traits[packet.Packet]{
		Fork: func(value packet.Packet) packet.Packet { return value.Share() },
		Drop: func(value packet.Packet) { value.Release() },
	})
	skeletonFrameSchema = schema.Define[skeletonFrameID, audio.Frame[int16]](schema.Traits[audio.Frame[int16]]{
		Fork: func(value audio.Frame[int16]) audio.Frame[int16] { return value.Share() },
		Drop: func(value audio.Frame[int16]) { value.Release() },
	})
	skeletonMetadataEventSchema = schema.Define[skeletonMetadataEventID, skeletonMetadataEvent](schema.Traits[skeletonMetadataEvent]{})
	skeletonMetadataCarrier     = carrier.Define[skeletonMetadataCarrierID]()
	skeletonSampleRate          = property.Define[skeletonSampleRateID](property.Scalar[int]())
	skeletonDecodedTimeBase     = timing.MustBase(1, 48000)
	skeletonEncodedTimeBase     = timing.MustBase(1, 1000)
)

type skeletonSourceOperator struct {
	shape   flow.Shape
	data    []byte
	emitted bool
}

func (o *skeletonSourceOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonSourceOperator) Close() error      { return nil }

func (o *skeletonSourceOperator) Read(_ context.Context, into *flow.Item[[]byte]) error {
	if o.emitted {
		return io.EOF
	}
	o.emitted = true
	*into = flow.NewItem(append([]byte(nil), o.data...), skeletonBytesSchema)
	return nil
}

type skeletonDemuxerOperator struct{ shape flow.Shape }

func (o *skeletonDemuxerOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonDemuxerOperator) Close() error      { return nil }

func (o *skeletonDemuxerOperator) Process(ctx context.Context, input *flow.Item[[]byte], output flow.Emitter[packet.Chunk]) error {
	if !input.Valid() {
		return fmt.Errorf("demuxer input was not owned")
	}
	data := input.Value()
	const bytesPerChunk = 2
	for offset, sequence := 0, uint64(0); offset < len(data); offset, sequence = offset+bytesPerChunk, sequence+1 {
		end := offset + bytesPerChunk
		if end > len(data) {
			end = len(data)
		}
		allocator, err := buffer.NewAllocator(int64(len(data) + 8))
		if err != nil {
			return err
		}
		payload, err := allocator.FromBytes(data[offset:end], 8)
		if err != nil {
			return err
		}
		chunk := packet.NewChunk(sequence, timing.SomePTS(timing.NewPTS(int64(sequence)*48)), payload)
		item := flow.NewItem(chunk, skeletonChunkSchema)
		if err := output.Emit(ctx, &item); err != nil {
			item.Drop()
			return err
		}
	}
	input.Drop()
	return nil
}

func (o *skeletonDemuxerOperator) Flush(context.Context, flow.Emitter[packet.Chunk]) error {
	return nil
}

type skeletonParserOperator struct{ shape flow.Shape }

func (o *skeletonParserOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonParserOperator) Close() error      { return nil }

func (o *skeletonParserOperator) Process(ctx context.Context, input *flow.Item[packet.Chunk], output flow.Emitter[packet.Packet]) error {
	if !input.Valid() {
		return fmt.Errorf("parser input was not owned")
	}
	chunk := input.Value()
	payload := chunk.Payload().Share()
	value := packet.NewPacket(chunk.Sequence(), chunk.PTS(), timing.UnknownDTS(), timing.SomeDuration(timing.NewDuration(int64(len(chunk.Bytes())/2))), payload)
	item := flow.NewItem(value, skeletonPacketSchema)
	if err := output.Emit(ctx, &item); err != nil {
		item.Drop()
		return err
	}
	input.Drop()
	return nil
}

func (o *skeletonParserOperator) Flush(context.Context, flow.Emitter[packet.Packet]) error {
	return nil
}

type skeletonCodecOperator struct{ shape flow.Shape }

func (o *skeletonCodecOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonCodecOperator) Close() error      { return nil }

func (o *skeletonCodecOperator) Process(ctx context.Context, input *flow.Item[packet.Packet], output flow.Emitter[audio.Frame[int16]]) error {
	if !input.Valid() {
		return fmt.Errorf("decoder input was not owned")
	}
	value := input.Value()
	payload := value.Payload().Share()
	frame, err := audio.NewFrame[int16](value.PTS(), len(value.Bytes())/2, payload)
	if err != nil {
		payload.Release()
		return err
	}
	item := flow.NewItem(frame, skeletonFrameSchema)
	if err := output.Emit(ctx, &item); err != nil {
		item.Drop()
		return err
	}
	input.Drop()
	return nil
}

func (o *skeletonCodecOperator) Flush(context.Context, flow.Emitter[audio.Frame[int16]]) error {
	return nil
}

type skeletonEncoderOperator struct {
	shape      flow.Shape
	pending    packet.Packet
	hasPending bool
	inputBase  timing.Base
	outputBase timing.Base
}

func (o *skeletonEncoderOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonEncoderOperator) Close() error      { return nil }

func (o *skeletonEncoderOperator) Process(ctx context.Context, input *flow.Item[audio.Frame[int16]], output flow.Emitter[packet.Packet]) error {
	if !input.Valid() {
		return fmt.Errorf("encoder input was not owned")
	}
	frame := input.Value()
	pts, err := frame.PTS().Rescale(o.inputBase, o.outputBase, timing.RoundNearestEven)
	if err != nil {
		return err
	}
	duration, err := timing.SomeDuration(timing.NewDuration(int64(frame.Samples()))).Rescale(o.inputBase, o.outputBase, timing.RoundNearestEven)
	if err != nil {
		return err
	}
	payload := frame.Planes().Share()
	value := packet.NewPacket(uint64(pts.Value()), pts, timing.UnknownDTS(), duration, payload)
	if o.hasPending {
		item := flow.NewItem(o.pending, skeletonPacketSchema)
		if err := output.Emit(ctx, &item); err != nil {
			item.Drop()
			value.Release()
			o.pending = packet.Packet{}
			o.hasPending = false
			return err
		}
	}
	o.pending = value
	o.hasPending = true
	input.Drop()
	return nil
}

func (o *skeletonEncoderOperator) Flush(ctx context.Context, output flow.Emitter[packet.Packet]) error {
	if !o.hasPending {
		return nil
	}
	item := flow.NewItem(o.pending, skeletonPacketSchema)
	o.hasPending = false
	if err := output.Emit(ctx, &item); err != nil {
		item.Drop()
		return err
	}
	return nil
}

type skeletonTrace struct {
	sequences     []uint64
	timestamps    []timing.PTS
	propertyReads []skeletonPropertyRead
}

type skeletonPropertyRead struct {
	component string
	id        key.ID
}

type skeletonDescriptorPorts struct {
	component string
	inputs    map[string]stream.Descriptor
	outputs   map[string]stream.Descriptor
	reads     map[key.ID]struct{}
}

func (p skeletonDescriptorPorts) validate(shape flow.Shape) error {
	if err := validateSkeletonDescriptorPorts(p.component, "input", shape.Inputs, p.inputs); err != nil {
		return err
	}
	return validateSkeletonDescriptorPorts(p.component, "output", shape.Outputs, p.outputs)
}

func validateSkeletonDescriptorPorts(component, direction string, ports []flow.Port, descriptors map[string]stream.Descriptor) error {
	if len(ports) != len(descriptors) {
		return fmt.Errorf("%s has %d %s descriptor(s), want %d", component, len(descriptors), direction, len(ports))
	}
	for _, port := range ports {
		descriptor, ok := descriptors[port.ID()]
		if !ok {
			return fmt.Errorf("%s %s port %q has no descriptor", component, direction, port.ID())
		}
		if !descriptor.Valid() || descriptor.Schema() != port.Schema().Identity() {
			return fmt.Errorf("%s %s port %q descriptor does not match its schema", component, direction, port.ID())
		}
	}
	return nil
}

func (p skeletonDescriptorPorts) accept(port string, descriptor stream.Descriptor) error {
	expected, ok := p.inputs[port]
	if !ok {
		return fmt.Errorf("%s has no input descriptor for port %q", p.component, port)
	}
	if descriptor.ID() != expected.ID() || descriptor.Schema() != expected.Schema() || descriptor.TimeBase() != expected.TimeBase() {
		return fmt.Errorf("%s input descriptor for port %q changed in transit", p.component, port)
	}
	return nil
}

func readSkeletonProperty[T any](stage skeletonDescriptorPorts, descriptor stream.Descriptor, declaration property.Key[T]) (T, error) {
	if _, ok := stage.reads[declaration.ID()]; !ok {
		var zero T
		return zero, fmt.Errorf("%s read undeclared property %s", stage.component, declaration.ID())
	}
	value, ok := declaration.Get(descriptor.Properties())
	if !ok {
		var zero T
		return zero, fmt.Errorf("%s did not receive declared property %s", stage.component, declaration.ID())
	}
	return value, nil
}

type skeletonDescriptorPath struct {
	source   skeletonDescriptorPorts
	demuxer  skeletonDescriptorPorts
	parser   skeletonDescriptorPorts
	parserID job.NodeID
	decoder  skeletonDescriptorPorts
	encoder  skeletonDescriptorPorts
	muxer    skeletonDescriptorPorts
}

func compiledSkeletonDescriptorPath(compiled program.Program) (skeletonDescriptorPath, error) {
	source, err := compiledSkeletonPorts(compiled, "source", "source", nil)
	if err != nil {
		return skeletonDescriptorPath{}, err
	}
	demuxer, err := compiledSkeletonPorts(compiled, "demuxer", "demuxer", nil)
	if err != nil {
		return skeletonDescriptorPath{}, err
	}
	parserID, err := compiledSkeletonNode(compiled, plugin.IdentityOf[skeletonParserID]())
	if err != nil {
		return skeletonDescriptorPath{}, err
	}
	parser, err := compiledSkeletonPorts(compiled, parserID, "parser", nil)
	if err != nil {
		return skeletonDescriptorPath{}, err
	}
	decoder, err := compiledSkeletonPorts(compiled, "decoder", "decoder", map[key.ID]struct{}{skeletonSampleRate.ID(): {}})
	if err != nil {
		return skeletonDescriptorPath{}, err
	}
	encoder, err := compiledSkeletonPorts(compiled, "encoder", "encoder", nil)
	if err != nil {
		return skeletonDescriptorPath{}, err
	}
	muxer, err := compiledSkeletonPorts(compiled, "muxer", "muxer", nil)
	if err != nil {
		return skeletonDescriptorPath{}, err
	}
	return skeletonDescriptorPath{source: source, demuxer: demuxer, parser: parser, parserID: parserID, decoder: decoder, encoder: encoder, muxer: muxer}, nil
}

func compiledSkeletonPorts(compiled program.Program, id job.NodeID, component string, reads map[key.ID]struct{}) (skeletonDescriptorPorts, error) {
	node, ok := compiled.Lookup(id)
	if !ok {
		return skeletonDescriptorPorts{}, fmt.Errorf("compiled Program has no %s node", id)
	}
	inputs, err := skeletonDescriptorMap(node.Inputs())
	if err != nil {
		return skeletonDescriptorPorts{}, fmt.Errorf("%s inputs: %w", component, err)
	}
	outputs, err := skeletonDescriptorMap(node.Outputs())
	if err != nil {
		return skeletonDescriptorPorts{}, fmt.Errorf("%s outputs: %w", component, err)
	}
	return skeletonDescriptorPorts{component: component, inputs: inputs, outputs: outputs, reads: reads}, nil
}

func compiledSkeletonNode(compiled program.Program, component plugin.Identity) (job.NodeID, error) {
	for _, node := range compiled.Nodes() {
		if node.Component() == component {
			return node.ID(), nil
		}
	}
	return "", fmt.Errorf("compiled Program has no component %s", component)
}

func skeletonDescriptorMap(descriptors flow.Descriptors[stream.Descriptor]) (map[string]stream.Descriptor, error) {
	result := make(map[string]stream.Descriptor, descriptors.Len())
	for _, binding := range descriptors.Bindings() {
		if _, exists := result[binding.Port()]; exists {
			return nil, fmt.Errorf("port %q has more than one descriptor", binding.Port())
		}
		result[binding.Port()] = binding.Descriptor()
	}
	return result, nil
}

func newSkeletonDemuxedDescriptor(source stream.Descriptor) (stream.Descriptor, error) {
	if source.Schema() != skeletonBytesSchema.Identity() {
		return stream.Descriptor{}, fmt.Errorf("demuxer source schema = %s", source.Schema())
	}
	properties, err := skeletonSampleRate.Set(property.New(), 48000)
	if err != nil {
		return stream.Descriptor{}, err
	}
	document, err := metadata.Add(metadata.NewBuilder(metadata.StreamScope), tag.Title(), "skeleton stream", metadata.Origin{}).Build()
	if err != nil {
		return stream.Descriptor{}, err
	}
	return newSkeletonDescriptor("audio-0", skeletonChunkSchema.Identity(), skeletonDecodedTimeBase, properties, document)
}

func newSkeletonDescriptor(id stream.ID, identity schema.ID, base timing.Base, properties property.Set, document metadata.Document) (stream.Descriptor, error) {
	descriptor, err := stream.NewDescriptor(id, identity, base, properties)
	if err != nil {
		return stream.Descriptor{}, err
	}
	return descriptor.WithMetadata(document), nil
}

func deriveSkeletonDescriptor(input stream.Descriptor, identity schema.ID, base timing.Base) (stream.Descriptor, error) {
	return newSkeletonDescriptor(input.ID(), identity, base, input.Properties(), input.Metadata())
}

type skeletonMuxerOperator struct {
	shape flow.Shape
	trace *skeletonTrace
}

func (o *skeletonMuxerOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonMuxerOperator) Close() error      { return nil }

func (o *skeletonMuxerOperator) Process(ctx context.Context, input *flow.Item[packet.Packet], output flow.Emitter[packet.Chunk]) error {
	if !input.Valid() {
		return fmt.Errorf("muxer input was not owned")
	}
	value := input.Value()
	payload := value.Payload().Share()
	chunk := packet.NewChunk(value.Sequence(), value.PTS(), payload)
	item := flow.NewItem(chunk, skeletonChunkSchema)
	if err := output.Emit(ctx, &item); err != nil {
		item.Drop()
		return err
	}
	if o.trace != nil {
		o.trace.sequences = append(o.trace.sequences, chunk.Sequence())
		o.trace.timestamps = append(o.trace.timestamps, chunk.PTS().Value())
	}
	input.Drop()
	return nil
}

func (o *skeletonMuxerOperator) Flush(context.Context, flow.Emitter[packet.Chunk]) error {
	return nil
}

const (
	skeletonMetadataTitle  byte = 1
	skeletonMetadataArtist byte = 2
	skeletonMetadataDate   byte = 3
)

type skeletonMetadataEvent struct {
	At    timing.PTS
	Key   key.ID
	Value string
}

func parseSkeletonMetadata(ctx metadata.ParseContext) (metadata.Document, error) {
	payload := ctx.Payload().AppendTo(nil)
	builder := metadata.NewBuilder(ctx.Scope())
	for offset := 0; offset < len(payload); {
		if len(payload)-offset < 2 {
			return metadata.Document{}, fmt.Errorf("metadata record at %d is truncated", offset)
		}
		kind := payload[offset]
		length := int(payload[offset+1])
		end := offset + 2 + length
		if end > len(payload) {
			return metadata.Document{}, fmt.Errorf("metadata record at %d exceeds payload", offset)
		}
		record := append([]byte(nil), payload[offset:end]...)
		value := payload[offset+2 : end]
		origin := metadata.Origin{Encoding: ctx.Encoding(), Carrier: ctx.Carrier()}
		switch kind {
		case skeletonMetadataTitle:
			origin.Native = "TITLE"
			metadata.Add(builder, tag.Title(), string(value), origin)
		case skeletonMetadataArtist:
			origin.Native = "ARTIST"
			metadata.Add(builder, tag.Artist(), string(value), origin)
		case skeletonMetadataDate:
			date, err := tag.ParseDate(string(value))
			if err != nil {
				builder.AddBlock(metadata.NewRawBlock(metadata.BlockID(fmt.Sprintf("%s-raw-%d", ctx.Block(), offset)), ctx.Carrier(), ctx.Encoding(), metadata.NewBlob("", record)))
				break
			}
			origin.Native = "DATE"
			metadata.Add(builder, tag.Date(), date, origin)
		default:
			builder.AddBlock(metadata.NewRawBlock(metadata.BlockID(fmt.Sprintf("%s-raw-%d", ctx.Block(), offset)), ctx.Carrier(), ctx.Encoding(), metadata.NewBlob("", record)))
		}
		offset = end
	}
	return builder.Build()
}

func marshalSkeletonMetadata(ctx metadata.MarshalContext) (metadata.Blob, error) {
	document := ctx.Document()
	result := make([]byte, 0, document.Len()*8)
	for _, entry := range document.Entries() {
		switch entry.Key() {
		case tag.Title().ID():
			value, ok := entry.Value().(string)
			if !ok {
				return metadata.Blob{}, fmt.Errorf("metadata title entry has type %T", entry.Value())
			}
			result = appendSkeletonMetadataRecord(result, skeletonMetadataTitle, []byte(value))
		case tag.Artist().ID():
			value, ok := entry.Value().(string)
			if !ok {
				return metadata.Blob{}, fmt.Errorf("metadata artist entry has type %T", entry.Value())
			}
			result = appendSkeletonMetadataRecord(result, skeletonMetadataArtist, []byte(value))
		case tag.Date().ID():
			value, ok := entry.Value().(tag.PartialDate)
			if !ok {
				return metadata.Blob{}, fmt.Errorf("metadata date entry has type %T", entry.Value())
			}
			result = appendSkeletonMetadataRecord(result, skeletonMetadataDate, []byte(value.ToISOString()))
		default:
			return metadata.Blob{}, fmt.Errorf("metadata key %s cannot be represented by fixture encoding", entry.Key())
		}
	}
	for _, block := range document.Blocks() {
		if block.Carrier() != ctx.Carrier() || block.Encoding() != ctx.Encoding() {
			return metadata.Blob{}, fmt.Errorf("raw block %s does not belong to fixture encoding", block.ID())
		}
		result = append(result, block.Payload().AppendTo(nil)...)
	}
	return metadata.NewBlob("application/octet-stream", result), nil
}

func appendSkeletonMetadataRecord(destination []byte, kind byte, value []byte) []byte {
	if len(value) > 255 {
		panic("foundation metadata fixture value exceeds one-byte length")
	}
	destination = append(destination, kind, byte(len(value)))
	return append(destination, value...)
}

type skeletonByteWriter struct {
	bytes []byte
}

func (w *skeletonByteWriter) Write(_ context.Context, input *flow.Item[[]byte]) error {
	defer input.Drop()
	if !input.Valid() {
		return fmt.Errorf("byte writer input was not owned")
	}
	w.bytes = append(w.bytes, input.Value()...)
	return nil
}

type skeletonChunkWriter struct {
	sink  flow.Writer[[]byte]
	items int
}

func (w *skeletonChunkWriter) Write(ctx context.Context, input *flow.Item[packet.Chunk]) error {
	defer input.Drop()
	if !input.Valid() {
		return fmt.Errorf("chunk writer input was not owned")
	}
	bytes := append([]byte(nil), input.Value().Bytes()...)
	byteInput := flow.NewItem(bytes, skeletonBytesSchema)
	defer byteInput.Drop()
	if err := w.sink.Write(ctx, &byteInput); err != nil {
		return err
	}
	w.items++
	return nil
}

type skeletonWriterEmitter[T any] struct{ sink flow.Writer[T] }

func (e skeletonWriterEmitter[T]) Emit(ctx context.Context, input *flow.Item[T]) error {
	return e.sink.Write(ctx, input)
}

type skeletonEmitter[T any] struct {
	items []flow.Owned[T]
}

func (e *skeletonEmitter[T]) Emit(_ context.Context, input *flow.Item[T]) error {
	e.items = append(e.items, input.Consume())
	return nil
}

type skeletonItem[T any] struct {
	input      flow.Owned[T]
	descriptor stream.Descriptor
}

func skeletonConfigSchema() config.Schema[skeletonConfig] {
	return config.Struct[skeletonConfig](func() skeletonConfig { return skeletonConfig{} }).
		Version("1").
		AddField(config.Field("value", func(value *skeletonConfig) *int { return &value.Value }, config.Int())).
		Build()
}

type skeletonPlan struct {
	shape         flow.Shape
	data          []byte
	trace         *skeletonTrace
	inputBase     timing.Base
	outputBase    timing.Base
	propertyReads []key.ID
}

type skeletonPlanner func(flow.Descriptors[stream.Descriptor]) (skeletonPlan, flow.Descriptors[stream.Descriptor], []plugin.Requirement[stream.Descriptor], error)

func skeletonSpec(shape flow.Shape, effect plugin.Effect, plan skeletonPlanner, open plugin.OpenFunc[skeletonPlan]) plugin.Spec[skeletonConfig, skeletonPlan, stream.Descriptor] {
	return plugin.Spec[skeletonConfig, skeletonPlan, stream.Descriptor]{
		Shape: plugin.StaticShape[skeletonConfig](shape),
		Compile: func(_ plugin.CompileContext, _ skeletonConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[skeletonPlan, stream.Descriptor], error) {
			compiledPlan, outputs, requirements, err := plan(inputs)
			compiledPlan.shape = shape.Clone()
			result := plugin.Compiled[skeletonPlan, stream.Descriptor]{
				Plan:         compiledPlan,
				Outputs:      outputs,
				Requirements: requirements,
			}
			if effect.Valid() {
				result.Effects = []plugin.Effect{effect}
			}
			return result, err
		},
		Open: open,
	}
}

func skeletonSourcePlanner(outputPort string, data []byte, descriptor func() (stream.Descriptor, error)) skeletonPlanner {
	data = append([]byte(nil), data...)
	return func(flow.Descriptors[stream.Descriptor]) (skeletonPlan, flow.Descriptors[stream.Descriptor], []plugin.Requirement[stream.Descriptor], error) {
		output, err := descriptor()
		return skeletonPlan{data: append([]byte(nil), data...)}, flow.NewDescriptors(flow.Describe(outputPort, output)), nil, err
	}
}

type skeletonDescriptorTransform func(stream.Descriptor) (stream.Descriptor, skeletonPlan, error)

func skeletonTransformPlanner(inputPort, outputPort string, transform skeletonDescriptorTransform) skeletonPlanner {
	return func(inputs flow.Descriptors[stream.Descriptor]) (skeletonPlan, flow.Descriptors[stream.Descriptor], []plugin.Requirement[stream.Descriptor], error) {
		input, ok := inputs.One(inputPort)
		if !ok {
			return skeletonPlan{}, flow.NewDescriptors[stream.Descriptor](), []plugin.Requirement[stream.Descriptor]{
				plugin.Require(inputPort, plugin.ConditionNeed[stream.Descriptor]("skeleton.input")),
			}, nil
		}
		output, plan, err := transform(input)
		return plan, flow.NewDescriptors(flow.Describe(outputPort, output)), nil, err
	}
}

func skeletonSinkPlanner(inputPort string) skeletonPlanner {
	return func(inputs flow.Descriptors[stream.Descriptor]) (skeletonPlan, flow.Descriptors[stream.Descriptor], []plugin.Requirement[stream.Descriptor], error) {
		if _, ok := inputs.One(inputPort); !ok {
			return skeletonPlan{}, flow.NewDescriptors[stream.Descriptor](), []plugin.Requirement[stream.Descriptor]{
				plugin.Require(inputPort, plugin.ConditionNeed[stream.Descriptor]("skeleton.input")),
			}, nil
		}
		return skeletonPlan{}, flow.NewDescriptors[stream.Descriptor](), nil, nil
	}
}

func skeletonTransform(identity schema.ID, base timing.Base) skeletonDescriptorTransform {
	return func(input stream.Descriptor) (stream.Descriptor, skeletonPlan, error) {
		output, err := deriveSkeletonDescriptor(input, identity, base)
		return output, skeletonPlan{}, err
	}
}

type skeletonNoopOperator struct{ shape flow.Shape }

func (o skeletonNoopOperator) Ports() flow.Shape { return o.shape.Clone() }
func (skeletonNoopOperator) Close() error        { return nil }

func skeletonComponents(data []byte, trace *skeletonTrace) plugin.Definition {
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("bytes", skeletonBytesSchema)})
	demuxerShape := flow.NewShape([]flow.Port{flow.In("bytes", skeletonBytesSchema)}, []flow.Port{flow.Out("chunks", skeletonChunkSchema, flow.Many())})
	parserShape := flow.NewShape([]flow.Port{flow.In("chunks", skeletonChunkSchema)}, []flow.Port{flow.Out("packets", skeletonPacketSchema)})
	decoderShape := flow.NewShape([]flow.Port{flow.In("packets", skeletonPacketSchema)}, []flow.Port{flow.Out("frames", skeletonFrameSchema)})
	encoderShape := flow.NewShape([]flow.Port{flow.In("frames", skeletonFrameSchema)}, []flow.Port{flow.Out("packets", skeletonPacketSchema)})
	muxerShape := flow.NewShape([]flow.Port{flow.In("packets", skeletonPacketSchema)}, []flow.Port{flow.Out("chunks", skeletonChunkSchema)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("chunks", skeletonChunkSchema)}, nil)
	configSchema := skeletonConfigSchema()
	structural := func(detail string) plugin.Effect {
		return plugin.Effect{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: detail}
	}
	return plugin.Define[skeletonPluginID](plugin.Descriptor{DisplayName: "foundation skeleton", Version: "1"},
		plugin.NewComponent[skeletonSourceID](plugin.Descriptor{DisplayName: "source"}, configSchema, plugin.WithSpec(skeletonSpec(sourceShape, structural("source"), skeletonSourcePlanner("bytes", data, func() (stream.Descriptor, error) {
			return newSkeletonDescriptor("source-0", skeletonBytesSchema.Identity(), timing.MustBase(1, 1), property.New(), metadata.Document{})
		}), func(_ plugin.OpenContext, plan skeletonPlan) (flow.Operator, error) {
			return &skeletonSourceOperator{shape: plan.shape, data: append([]byte(nil), plan.data...)}, nil
		}))),
		plugin.NewComponent[skeletonDemuxerID](plugin.Descriptor{DisplayName: "demuxer"}, configSchema, plugin.WithSpec(skeletonSpec(demuxerShape, structural("demux"), skeletonTransformPlanner("bytes", "chunks", func(input stream.Descriptor) (stream.Descriptor, skeletonPlan, error) {
			output, err := newSkeletonDemuxedDescriptor(input)
			return output, skeletonPlan{}, err
		}), func(_ plugin.OpenContext, plan skeletonPlan) (flow.Operator, error) {
			return &skeletonDemuxerOperator{shape: plan.shape}, nil
		}))),
		plugin.NewComponent[skeletonParserID](plugin.Descriptor{DisplayName: "parser"}, configSchema, plugin.WithSpec(skeletonSpec(parserShape, structural("parse"), skeletonTransformPlanner("chunks", "packets", skeletonTransform(skeletonPacketSchema.Identity(), skeletonDecodedTimeBase)), func(_ plugin.OpenContext, plan skeletonPlan) (flow.Operator, error) {
			return &skeletonParserOperator{shape: plan.shape}, nil
		}))),
		plugin.NewComponent[skeletonCodecID](plugin.Descriptor{DisplayName: "decoder"}, configSchema, plugin.WithSpec(skeletonSpec(decoderShape, plugin.Effect{Kind: plugin.CompressionEffect, Loss: plugin.NoLoss, Detail: "decode"}, skeletonTransformPlanner("packets", "frames", func(input stream.Descriptor) (stream.Descriptor, skeletonPlan, error) {
			stage := skeletonDescriptorPorts{component: "decoder", reads: map[key.ID]struct{}{skeletonSampleRate.ID(): {}}}
			rate, err := readSkeletonProperty(stage, input, skeletonSampleRate)
			if err != nil {
				return stream.Descriptor{}, skeletonPlan{}, err
			}
			output, err := deriveSkeletonDescriptor(input, skeletonFrameSchema.Identity(), timing.MustBase(1, int64(rate)))
			return output, skeletonPlan{trace: trace, propertyReads: []key.ID{skeletonSampleRate.ID()}}, err
		}), func(_ plugin.OpenContext, plan skeletonPlan) (flow.Operator, error) {
			if plan.trace != nil {
				for _, id := range plan.propertyReads {
					plan.trace.propertyReads = append(plan.trace.propertyReads, skeletonPropertyRead{component: "decoder", id: id})
				}
			}
			return &skeletonCodecOperator{shape: plan.shape}, nil
		}))),
		plugin.NewComponent[skeletonEncoderID](plugin.Descriptor{DisplayName: "encoder"}, configSchema, plugin.WithSpec(skeletonSpec(encoderShape, plugin.Effect{Kind: plugin.CompressionEffect, Loss: plugin.Lossy, Detail: "encode"}, skeletonTransformPlanner("frames", "packets", func(input stream.Descriptor) (stream.Descriptor, skeletonPlan, error) {
			output, err := deriveSkeletonDescriptor(input, skeletonPacketSchema.Identity(), skeletonEncodedTimeBase)
			return output, skeletonPlan{inputBase: skeletonDecodedTimeBase, outputBase: skeletonEncodedTimeBase}, err
		}), func(_ plugin.OpenContext, plan skeletonPlan) (flow.Operator, error) {
			return &skeletonEncoderOperator{shape: plan.shape, inputBase: plan.inputBase, outputBase: plan.outputBase}, nil
		}))),
		plugin.NewComponent[skeletonMuxerID](plugin.Descriptor{DisplayName: "muxer"}, configSchema, plugin.WithSpec(skeletonSpec(muxerShape, structural("mux"), skeletonTransformPlanner("packets", "chunks", func(input stream.Descriptor) (stream.Descriptor, skeletonPlan, error) {
			output, err := deriveSkeletonDescriptor(input, skeletonChunkSchema.Identity(), skeletonEncodedTimeBase)
			return output, skeletonPlan{trace: trace}, err
		}), func(_ plugin.OpenContext, plan skeletonPlan) (flow.Operator, error) {
			return &skeletonMuxerOperator{shape: plan.shape, trace: plan.trace}, nil
		}))),
		plugin.NewComponent[skeletonSinkID](plugin.Descriptor{DisplayName: "sink"}, configSchema, plugin.WithSpec(skeletonSpec(sinkShape, structural("sink"), skeletonSinkPlanner("chunks"), func(_ plugin.OpenContext, plan skeletonPlan) (flow.Operator, error) {
			return skeletonNoopOperator{shape: plan.shape}, nil
		}))),
		plugin.NewComponent[skeletonMetadataEncodingID](plugin.Descriptor{DisplayName: "metadata encoding"}, configSchema, metadata.WithEncoding(parseSkeletonMetadata, marshalSkeletonMetadata)),
	)
}

func skeletonBinding() codec.Binding {
	return codec.Bind(format.NewTag("fixture", "bytes"), codec.Define[skeletonCodecID](), codec.DefineParser[skeletonParserID]())
}

func skeletonMetadataBinding() metadata.Binding {
	return metadata.Bind(skeletonMetadataCarrier, plugin.IdentityOf[skeletonMetadataEncodingID]())
}

// skeletonCatalog validates a composition the same way host.New does. The
// skeleton retains the private index so it can inspect the Program selected by
// the planner without exposing executable state from Host.
func skeletonCatalog(set plugin.Set) (catalog.Index, error) { return catalog.Build(set) }

func compileSkeleton(index catalog.Index) (program.Program, error) {
	request, err := job.NewGraph(
		[]job.Node{
			job.NewNode("source", plugin.IdentityOf[skeletonSourceID](), config.NewPatch()),
			job.NewNode("demuxer", plugin.IdentityOf[skeletonDemuxerID](), config.NewPatch()),
			job.NewNode("decoder", plugin.IdentityOf[skeletonCodecID](), config.NewPatch()),
			job.NewNode("encoder", plugin.IdentityOf[skeletonEncoderID](), config.NewPatch()),
			job.NewNode("muxer", plugin.IdentityOf[skeletonMuxerID](), config.NewPatch()),
			job.NewNode("sink", plugin.IdentityOf[skeletonSinkID](), config.NewPatch()),
		},
		[]job.Edge{
			job.Connect(job.At("source", "bytes"), job.At("demuxer", "bytes")),
			job.Connect(job.At("demuxer", "chunks"), job.At("decoder", "packets")),
			job.Connect(job.At("decoder", "frames"), job.At("encoder", "frames")),
			job.Connect(job.At("encoder", "packets"), job.At("muxer", "packets")),
			job.Connect(job.At("muxer", "chunks"), job.At("sink", "chunks")),
		},
	)
	if err != nil {
		return program.Program{}, err
	}
	jobRequest, err := job.New(nil, nil, request)
	if err != nil {
		return program.Program{}, err
	}
	return solve.Resolve(context.Background(), index, jobRequest, plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"})
}

func TestWalkingSkeletonPreservesBytesTimingOrderAndOwnership(t *testing.T) {
	inputBytes := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	trace := &skeletonTrace{}
	definition := skeletonComponents(inputBytes, trace)
	trivialFormat, err := format.Define[skeletonFormatID]([]carrier.ID{carrier.Define[skeletonPayloadCarrierID]()})
	if err != nil || !trivialFormat.Valid() {
		t.Fatalf("trivial format = %#v, %v", trivialFormat, err)
	}
	index, err := skeletonCatalog(plugin.NewSet(definition).AddDeclaration(skeletonBinding()).AddDeclaration(skeletonMetadataBinding()))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileSkeleton(index)
	if err != nil {
		t.Fatal(err)
	}
	automatic := 0
	for _, node := range compiled.Plan().Nodes() {
		if node.Origin == plan.Automatic {
			automatic++
			if node.Component != plugin.IdentityOf[skeletonParserID]().String() || node.Reason != "graph.schema-mismatch" {
				t.Fatalf("automatic Plan node = %#v", node)
			}
		}
	}
	if automatic != 1 {
		t.Fatalf("automatic Plan nodes = %d, want parser only", automatic)
	}
	descriptors, err := compiledSkeletonDescriptorPath(compiled)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	openContext := plugin.NewOpenContext(ctx, plugin.OpenServices{})
	sourceValue, err := compiled.Open(openContext, "source")
	if err != nil {
		t.Fatal(err)
	}
	demuxerValue, err := compiled.Open(openContext, "demuxer")
	if err != nil {
		t.Fatal(err)
	}
	parserValue, err := compiled.Open(openContext, descriptors.parserID)
	if err != nil {
		t.Fatal(err)
	}
	decoderValue, err := compiled.Open(openContext, "decoder")
	if err != nil {
		t.Fatal(err)
	}
	encoderValue, err := compiled.Open(openContext, "encoder")
	if err != nil {
		t.Fatal(err)
	}
	muxerValue, err := compiled.Open(openContext, "muxer")
	if err != nil {
		t.Fatal(err)
	}
	defer sourceValue.Close()
	defer demuxerValue.Close()
	defer parserValue.Close()
	defer decoderValue.Close()
	defer encoderValue.Close()
	defer muxerValue.Close()
	for _, component := range []struct {
		operator    flow.Operator
		descriptors skeletonDescriptorPorts
	}{
		{sourceValue, descriptors.source},
		{demuxerValue, descriptors.demuxer},
		{parserValue, descriptors.parser},
		{decoderValue, descriptors.decoder},
		{encoderValue, descriptors.encoder},
		{muxerValue, descriptors.muxer},
	} {
		shape := component.operator.Ports()
		if err := shape.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := component.descriptors.validate(shape); err != nil {
			t.Fatal(err)
		}
	}
	source, ok := sourceValue.(flow.Reader[[]byte])
	if !ok {
		t.Fatal("source did not implement flow.Reader[[]byte]")
	}
	demuxer, ok := demuxerValue.(flow.Processor[[]byte, packet.Chunk])
	if !ok {
		t.Fatal("demuxer did not implement flow.Processor[[]byte, packet.Chunk]")
	}
	parser, ok := parserValue.(flow.Processor[packet.Chunk, packet.Packet])
	if !ok {
		t.Fatal("parser did not implement flow.Processor[packet.Chunk, packet.Packet]")
	}
	decoder, ok := decoderValue.(flow.Processor[packet.Packet, audio.Frame[int16]])
	if !ok {
		t.Fatal("decoder did not implement flow.Processor[packet.Packet, audio.Frame[int16]]")
	}
	encoder, ok := encoderValue.(flow.Processor[audio.Frame[int16], packet.Packet])
	if !ok {
		t.Fatal("encoder did not implement flow.Processor[audio.Frame[int16], packet.Packet]")
	}
	muxer, ok := muxerValue.(flow.Processor[packet.Packet, packet.Chunk])
	if !ok {
		t.Fatal("muxer did not implement flow.Processor[packet.Packet, packet.Chunk]")
	}
	byteWriter := &skeletonByteWriter{}
	var byteSink flow.Writer[[]byte] = byteWriter
	chunkWriter := &skeletonChunkWriter{sink: byteSink}
	var chunkSink flow.Writer[packet.Chunk] = chunkWriter
	var chunkEmitter flow.Emitter[packet.Chunk] = skeletonWriterEmitter[packet.Chunk]{sink: chunkSink}
	chunkOutput := &skeletonEmitter[packet.Chunk]{}
	packetOutput := &skeletonEmitter[packet.Packet]{}
	frameOutput := &skeletonEmitter[audio.Frame[int16]]{}
	encodedPacketOutput := &skeletonEmitter[packet.Packet]{}
	var byteCell flow.Item[[]byte]
	if err := source.Read(ctx, &byteCell); err != nil {
		t.Fatal(err)
	}
	byteItem := skeletonItem[[]byte]{input: byteCell.Consume(), descriptor: descriptors.source.outputs["bytes"]}
	if _, ok := skeletonSampleRate.Get(byteItem.descriptor.Properties()); ok {
		t.Fatal("demuxer input unexpectedly had the generated sample-rate property")
	}
	if err := descriptors.demuxer.accept("bytes", byteItem.descriptor); err != nil {
		t.Fatal(err)
	}
	if err := processOwned(ctx, byteItem.input, chunkOutput, demuxer.Process); err != nil {
		t.Fatal(err)
	}
	rate, ok := skeletonSampleRate.Get(descriptors.decoder.inputs["packets"].Properties())
	if !ok || rate != 48000 || descriptors.decoder.inputs["packets"].TimeBase().Denominator != int64(rate) {
		t.Fatalf("decoder descriptor rate = %d, time base = %s", rate, descriptors.decoder.inputs["packets"].TimeBase())
	}
	for _, chunkInput := range chunkOutput.items {
		chunk := skeletonItem[packet.Chunk]{input: chunkInput, descriptor: descriptors.demuxer.outputs["chunks"]}
		packetOutput.items = packetOutput.items[:0]
		if err := descriptors.parser.accept("chunks", chunk.descriptor); err != nil {
			t.Fatal(err)
		}
		if err := processOwned(ctx, chunk.input, packetOutput, parser.Process); err != nil {
			t.Fatal(err)
		}
		for _, packetInput := range packetOutput.items {
			packetValue := skeletonItem[packet.Packet]{input: packetInput, descriptor: descriptors.parser.outputs["packets"]}
			frameOutput.items = frameOutput.items[:0]
			if err := descriptors.decoder.accept("packets", packetValue.descriptor); err != nil {
				t.Fatal(err)
			}
			if err := processOwned(ctx, packetValue.input, frameOutput, decoder.Process); err != nil {
				t.Fatal(err)
			}
			for _, frameInput := range frameOutput.items {
				frame := skeletonItem[audio.Frame[int16]]{input: frameInput, descriptor: descriptors.decoder.outputs["frames"]}
				encodedPacketOutput.items = encodedPacketOutput.items[:0]
				if err := descriptors.encoder.accept("frames", frame.descriptor); err != nil {
					t.Fatal(err)
				}
				if err := processOwned(ctx, frame.input, encodedPacketOutput, encoder.Process); err != nil {
					t.Fatal(err)
				}
				for _, encodedPacketInput := range encodedPacketOutput.items {
					encodedPacket := skeletonItem[packet.Packet]{input: encodedPacketInput, descriptor: descriptors.encoder.outputs["packets"]}
					if err := descriptors.muxer.accept("packets", encodedPacket.descriptor); err != nil {
						t.Fatal(err)
					}
					if err := processOwned(ctx, encodedPacket.input, chunkEmitter, muxer.Process); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	encodedPacketOutput.items = encodedPacketOutput.items[:0]
	if err := encoder.Flush(ctx, encodedPacketOutput); err != nil {
		t.Fatal(err)
	}
	for _, encodedPacketInput := range encodedPacketOutput.items {
		encodedPacket := skeletonItem[packet.Packet]{input: encodedPacketInput, descriptor: descriptors.encoder.outputs["packets"]}
		if err := descriptors.muxer.accept("packets", encodedPacket.descriptor); err != nil {
			t.Fatal(err)
		}
		if err := processOwned(ctx, encodedPacket.input, chunkEmitter, muxer.Process); err != nil {
			t.Fatal(err)
		}
	}
	if err := muxer.Flush(ctx, chunkEmitter); err != nil {
		t.Fatal(err)
	}
	var tail flow.Item[[]byte]
	if err := source.Read(ctx, &tail); err != io.EOF {
		tail.Drop()
		t.Fatalf("second source read = %v, want EOF", err)
	}
	if string(byteWriter.bytes) != string(inputBytes) {
		t.Fatalf("round trip bytes = %v, want %v", byteWriter.bytes, inputBytes)
	}
	if chunkWriter.items != 4 || len(trace.sequences) != 4 {
		t.Fatalf("output items = %d, trace = %d, want 4", chunkWriter.items, len(trace.sequences))
	}
	for index := range trace.sequences {
		if trace.sequences[index] != uint64(index) || trace.timestamps[index] != timing.PTS(index) {
			t.Fatalf("item %d = sequence %d, pts %d", index, trace.sequences[index], trace.timestamps[index])
		}
	}
	terminal := descriptors.muxer.outputs["chunks"]
	if terminal.Schema() != skeletonChunkSchema.Identity() || terminal.TimeBase() != skeletonEncodedTimeBase {
		t.Fatalf("terminal descriptor = schema %s, time base %s", terminal.Schema(), terminal.TimeBase())
	}
	if value, ok := skeletonSampleRate.Get(terminal.Properties()); !ok || value != 48000 {
		t.Fatalf("terminal sample rate = %d, %v", value, ok)
	}
	if title, ok := metadata.First(terminal.Metadata(), tag.Title()); !ok || title != "skeleton stream" {
		t.Fatalf("terminal metadata title = %q, %v", title, ok)
	}
	if len(trace.propertyReads) != 1 || trace.propertyReads[0] != (skeletonPropertyRead{component: "decoder", id: skeletonSampleRate.ID()}) {
		t.Fatalf("component property reads = %#v", trace.propertyReads)
	}
}

func TestWalkingSkeletonMetadataEncodingPreservesRawAndOrder(t *testing.T) {
	index, err := skeletonCatalog(plugin.NewSet(skeletonComponents(nil, nil)).AddDeclaration(skeletonMetadataBinding()))
	if err != nil {
		t.Fatal(err)
	}
	component, ok := index.Lookup(plugin.IdentityOf[skeletonMetadataEncodingID]())
	if !ok {
		t.Fatal("metadata encoding component is absent from the catalog")
	}
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{skeletonMetadataCarrier: component})
	if err != nil {
		t.Fatal(err)
	}
	rawRecord := []byte{0xfe, 3, 0xde, 0xad, 0xbe}
	payload := appendSkeletonMetadataRecord(nil, skeletonMetadataTitle, []byte("Song"))
	payload = appendSkeletonMetadataRecord(payload, skeletonMetadataArtist, []byte("First"))
	payload = appendSkeletonMetadataRecord(payload, skeletonMetadataArtist, []byte("Second"))
	payload = append(payload, rawRecord...)

	document, err := resolver.Parse(t.Context(), skeletonMetadataCarrier, "fixture", metadata.StreamScope, metadata.NewBlob("application/octet-stream", payload))
	if err != nil {
		t.Fatal(err)
	}
	if document.Scope() != metadata.StreamScope || document.Len() != 3 {
		t.Fatalf("metadata document = scope %v, len %d", document.Scope(), document.Len())
	}
	if values := metadata.Values(document, tag.Artist()); len(values) != 2 || values[0] != "First" || values[1] != "Second" {
		t.Fatalf("artist order = %v", values)
	}
	if title, ok := metadata.First(document, tag.Title()); !ok || title != "Song" {
		t.Fatalf("title = %q, %v", title, ok)
	}
	blocks := document.Blocks()
	if len(blocks) != 1 || !bytes.Equal(blocks[0].Payload().AppendTo(nil), rawRecord) {
		t.Fatalf("raw blocks = %#v", blocks)
	}

	reencoded, err := resolver.Marshal(t.Context(), skeletonMetadataCarrier, "fixture", document)
	if err != nil {
		t.Fatal(err)
	}
	reencodedBytes := reencoded.AppendTo(nil)
	if !bytes.Contains(reencodedBytes, rawRecord) {
		t.Fatalf("reencoded metadata lost raw record: %x", reencodedBytes)
	}
	parsedAgain, err := resolver.Parse(t.Context(), skeletonMetadataCarrier, "roundtrip", metadata.StreamScope, reencoded)
	if err != nil {
		t.Fatal(err)
	}
	if values := metadata.Values(parsedAgain, tag.Artist()); len(values) != 2 || values[0] != "First" || values[1] != "Second" {
		t.Fatalf("roundtrip artist order = %v", values)
	}
}

func TestMetadataBindingUsesHostConflictAndTargetChecks(t *testing.T) {
	base := plugin.NewSet(skeletonComponents(nil, nil)).AddDeclaration(skeletonMetadataBinding())
	if _, err := host.New(host.Plugins(base)); err != nil {
		t.Fatalf("valid metadata binding rejected: %v", err)
	}

	conflict := metadata.Bind(skeletonMetadataCarrier, plugin.IdentityOf[skeletonDemuxerID]())
	if _, err := host.New(host.Plugins(base.AddDeclaration(conflict))); err == nil {
		t.Fatal("host accepted conflicting metadata binding")
	}

	missing := metadata.Bind(skeletonMetadataCarrier, plugin.IdentityOf[skeletonMissingMetadataID]())
	if _, err := host.New(host.Plugins(plugin.NewSet(skeletonComponents(nil, nil)).AddDeclaration(missing))); err == nil {
		t.Fatal("host accepted metadata binding with missing target")
	}
}

func TestTimedMetadataUsesTypedEventSchema(t *testing.T) {
	shape := flow.NewShape([]flow.Port{flow.In("metadata-events", skeletonMetadataEventSchema, flow.Many(), flow.WithFanIn(flow.MergeFanIn))}, nil)
	if err := shape.Validate(); err != nil {
		t.Fatal(err)
	}
	if shape.Inputs[0].Schema().Identity() != skeletonMetadataEventSchema.Identity() {
		t.Fatal("metadata event port lost its schema identity")
	}
	event := skeletonMetadataEvent{At: timing.PTS(7), Key: tag.Title().ID(), Value: "live"}
	if event.At != 7 || event.Key != tag.Title().ID() || event.Value != "live" {
		t.Fatalf("metadata event = %#v", event)
	}
}

func TestWalkingSkeletonRejectsConflictingBindingInHostBuild(t *testing.T) {
	base := plugin.NewSet(skeletonComponents(nil, nil)).AddDeclaration(skeletonBinding())
	conflict := codec.BindWithoutParser(format.NewTag("fixture", "bytes"), codec.New(plugin.IdentityOf[skeletonDemuxerID]()))
	if _, err := host.New(host.Plugins(base.AddDeclaration(conflict))); err == nil {
		t.Fatal("host accepted conflicting codec declaration")
	}
}

func TestThirdPartyNonAudioSchemasConnectWithoutCoreChanges(t *testing.T) {
	type videoID struct{}
	type videoUnit struct{ Width int }
	type subtitleID struct{}
	type subtitleCue struct{ Text string }
	video := schema.Define[videoID, videoUnit](schema.Traits[videoUnit]{
		Fork: func(value videoUnit) videoUnit { return value },
	})
	subtitle := schema.Define[subtitleID, subtitleCue](schema.Traits[subtitleCue]{})
	shape := flow.NewShape([]flow.Port{flow.In("video", video)}, []flow.Port{flow.Out("subtitle", subtitle)})
	if err := shape.Validate(); err != nil {
		t.Fatal(err)
	}
	if video.Identity() == subtitle.Identity() || video.Identity().IsZero() || subtitle.Identity().IsZero() {
		t.Fatal("third-party schema identities are not open and distinct")
	}
	if shape.Inputs[0].Schema().Identity() != video.Identity() {
		t.Fatal("port did not carry the declaring schema identity")
	}
}

// processOwned adopts stored ownership into a cell for one direct Process
// call, mirroring what a runtime delivery does around a bounded edge.
func processOwned[I, O any](ctx context.Context, owned flow.Owned[I], output flow.Emitter[O], process func(context.Context, *flow.Item[I], flow.Emitter[O]) error) error {
	var cell flow.Item[I]
	cell.Adopt(owned)
	defer cell.Drop()
	return process(ctx, &cell, output)
}
