package media_test

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type skeletonPluginID struct{}
type skeletonSourceID struct{}
type skeletonDemuxerID struct{}
type skeletonParserID struct{}
type skeletonCodecID struct{}
type skeletonEncoderID struct{}
type skeletonMuxerID struct{}
type skeletonFormatID struct{}
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
)

type skeletonSourceOperator struct {
	shape   flow.Shape
	data    []byte
	emitted bool
}

func (o *skeletonSourceOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonSourceOperator) Close() error      { return nil }

func (o *skeletonSourceOperator) Read(context.Context) (flow.Input[[]byte], error) {
	if o.emitted {
		return flow.Input[[]byte]{}, io.EOF
	}
	o.emitted = true
	return flow.NewInput(append([]byte(nil), o.data...), skeletonBytesSchema), nil
}

type skeletonDemuxerOperator struct{ shape flow.Shape }

func (o *skeletonDemuxerOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonDemuxerOperator) Close() error      { return nil }

func (o *skeletonDemuxerOperator) Process(ctx context.Context, input flow.Input[[]byte], output flow.Emitter[packet.Chunk]) error {
	owner := input.Take()
	if !owner.Valid() {
		return fmt.Errorf("demuxer input was not owned")
	}
	data := owner.Value()
	owner.Release()
	const bytesPerChunk = 2
	for offset, sequence := 0, uint64(0); offset < len(data); offset, sequence = offset+bytesPerChunk, sequence+1 {
		end := offset + bytesPerChunk
		if end > len(data) {
			end = len(data)
		}
		payload, err := buffer.FromBytes(data[offset:end], 8)
		if err != nil {
			return err
		}
		chunk := packet.NewTimestampedChunk(sequence, timing.SomePTS(timing.NewPTS(int64(sequence))), payload)
		item := flow.NewInput(chunk, skeletonChunkSchema)
		if err := output.Emit(ctx, item); err != nil {
			item.Drop()
			return err
		}
	}
	return nil
}

func (o *skeletonDemuxerOperator) Flush(context.Context, flow.Emitter[packet.Chunk]) error {
	return nil
}

type skeletonParserOperator struct{ shape flow.Shape }

func (o *skeletonParserOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonParserOperator) Close() error      { return nil }

func (o *skeletonParserOperator) Process(ctx context.Context, input flow.Input[packet.Chunk], output flow.Emitter[packet.Packet]) error {
	owner := input.Take()
	if !owner.Valid() {
		return fmt.Errorf("parser input was not owned")
	}
	chunk := owner.Value()
	payload := chunk.Payload().Share()
	value := packet.NewPacket(chunk.Sequence(), chunk.PTS(), timing.UnknownDTS(), timing.SomeDuration(timing.NewDuration(int64(len(chunk.Bytes())/2))), payload)
	owner.Release()
	item := flow.NewInput(value, skeletonPacketSchema)
	if err := output.Emit(ctx, item); err != nil {
		item.Drop()
		return err
	}
	return nil
}

func (o *skeletonParserOperator) Flush(context.Context, flow.Emitter[packet.Packet]) error {
	return nil
}

type skeletonCodecOperator struct{ shape flow.Shape }

func (o *skeletonCodecOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonCodecOperator) Close() error      { return nil }

func (o *skeletonCodecOperator) Process(ctx context.Context, input flow.Input[packet.Packet], output flow.Emitter[audio.Frame[int16]]) error {
	owner := input.Take()
	if !owner.Valid() {
		return fmt.Errorf("decoder input was not owned")
	}
	value := owner.Value()
	payload := value.Payload().Share()
	frame, err := audio.NewFrame[int16](value.PTS(), len(value.Bytes())/2, payload)
	if err != nil {
		payload.Release()
		owner.Release()
		return err
	}
	owner.Release()
	item := flow.NewInput(frame, skeletonFrameSchema)
	if err := output.Emit(ctx, item); err != nil {
		item.Drop()
		return err
	}
	return nil
}

func (o *skeletonCodecOperator) Flush(context.Context, flow.Emitter[audio.Frame[int16]]) error {
	return nil
}

type skeletonEncoderOperator struct {
	shape      flow.Shape
	pending    packet.Packet
	hasPending bool
}

func (o *skeletonEncoderOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonEncoderOperator) Close() error      { return nil }

func (o *skeletonEncoderOperator) Process(ctx context.Context, input flow.Input[audio.Frame[int16]], output flow.Emitter[packet.Packet]) error {
	owner := input.Take()
	if !owner.Valid() {
		return fmt.Errorf("encoder input was not owned")
	}
	frame := owner.Value()
	payload := frame.Planes().Share()
	value := packet.NewPacket(uint64(frame.PTS().Value()), frame.PTS(), timing.UnknownDTS(), timing.SomeDuration(timing.NewDuration(int64(frame.Samples()))), payload)
	owner.Release()
	if o.hasPending {
		item := flow.NewInput(o.pending, skeletonPacketSchema)
		o.hasPending = false
		if err := output.Emit(ctx, item); err != nil {
			item.Drop()
			value.Release()
			return err
		}
	}
	o.pending = value
	o.hasPending = true
	return nil
}

func (o *skeletonEncoderOperator) Flush(ctx context.Context, output flow.Emitter[packet.Packet]) error {
	if !o.hasPending {
		return nil
	}
	item := flow.NewInput(o.pending, skeletonPacketSchema)
	o.hasPending = false
	if err := output.Emit(ctx, item); err != nil {
		item.Drop()
		return err
	}
	return nil
}

type skeletonTrace struct {
	sequences  []uint64
	timestamps []timing.PTS
}

type skeletonMuxerOperator struct {
	shape flow.Shape
	trace *skeletonTrace
}

func (o *skeletonMuxerOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonMuxerOperator) Close() error      { return nil }

func (o *skeletonMuxerOperator) Process(ctx context.Context, input flow.Input[packet.Packet], output flow.Emitter[packet.Chunk]) error {
	owner := input.Take()
	if !owner.Valid() {
		return fmt.Errorf("muxer input was not owned")
	}
	value := owner.Value()
	payload := value.Payload().Share()
	chunk := packet.NewTimestampedChunk(value.Sequence(), value.PTS(), payload)
	owner.Release()
	if o.trace != nil {
		o.trace.sequences = append(o.trace.sequences, chunk.Sequence())
		o.trace.timestamps = append(o.trace.timestamps, chunk.PTS().Value())
	}
	item := flow.NewInput(chunk, skeletonChunkSchema)
	if err := output.Emit(ctx, item); err != nil {
		item.Drop()
		return err
	}
	return nil
}

func (o *skeletonMuxerOperator) Flush(context.Context, flow.Emitter[packet.Chunk]) error {
	return nil
}

type skeletonByteWriter struct {
	bytes []byte
}

func (w *skeletonByteWriter) Write(_ context.Context, input flow.Input[[]byte]) error {
	owner := input.Take()
	if !owner.Valid() {
		return fmt.Errorf("byte writer input was not owned")
	}
	w.bytes = append(w.bytes, owner.Value()...)
	owner.Release()
	return nil
}

type skeletonChunkWriter struct {
	sink  flow.Writer[[]byte]
	items int
}

func (w *skeletonChunkWriter) Write(ctx context.Context, input flow.Input[packet.Chunk]) error {
	if !input.Valid() {
		return fmt.Errorf("chunk writer input was not owned")
	}
	value := input.Value()
	bytes := append([]byte(nil), value.Bytes()...)
	byteInput := flow.NewInput(bytes, skeletonBytesSchema)
	if err := w.sink.Write(ctx, byteInput); err != nil {
		byteInput.Drop()
		return err
	}
	input.Drop()
	w.items++
	return nil
}

type skeletonWriterEmitter[T any] struct{ sink flow.Writer[T] }

func (e skeletonWriterEmitter[T]) Emit(ctx context.Context, input flow.Input[T]) error {
	return e.sink.Write(ctx, input)
}

type skeletonEmitter[T any] struct {
	items []flow.Input[T]
}

func (e *skeletonEmitter[T]) Emit(_ context.Context, input flow.Input[T]) error {
	e.items = append(e.items, input)
	return nil
}

func skeletonConfigSchema() config.Schema[skeletonConfig] {
	return config.Struct(func() skeletonConfig { return skeletonConfig{} }).
		Identity("media.foundation.skeleton").
		Version("1").
		AddField(config.Field("value", func(value *skeletonConfig) *int { return &value.Value }, config.Int())).
		Build()
}

func skeletonComponents(data []byte, trace *skeletonTrace) plugin.Definition {
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("bytes", skeletonBytesSchema)})
	demuxerShape := flow.NewShape([]flow.Port{flow.In("bytes", skeletonBytesSchema)}, []flow.Port{flow.Out("chunks", skeletonChunkSchema, flow.Many())})
	parserShape := flow.NewShape([]flow.Port{flow.In("chunks", skeletonChunkSchema)}, []flow.Port{flow.Out("packets", skeletonPacketSchema)})
	decoderShape := flow.NewShape([]flow.Port{flow.In("packets", skeletonPacketSchema)}, []flow.Port{flow.Out("frames", skeletonFrameSchema)})
	encoderShape := flow.NewShape([]flow.Port{flow.In("frames", skeletonFrameSchema)}, []flow.Port{flow.Out("packets", skeletonPacketSchema)})
	muxerShape := flow.NewShape([]flow.Port{flow.In("packets", skeletonPacketSchema)}, []flow.Port{flow.Out("chunks", skeletonChunkSchema)})
	configSchema := skeletonConfigSchema()
	return plugin.Define[skeletonPluginID](plugin.Descriptor{DisplayName: "foundation skeleton", Version: "1"},
		plugin.NewComponent[skeletonSourceID](plugin.Descriptor{DisplayName: "source"}, configSchema, plugin.WithPorts(sourceShape), plugin.WithOpen(func() (flow.Operator, error) {
			return &skeletonSourceOperator{shape: sourceShape, data: append([]byte(nil), data...)}, nil
		})),
		plugin.NewComponent[skeletonDemuxerID](plugin.Descriptor{DisplayName: "demuxer"}, configSchema, plugin.WithPorts(demuxerShape), plugin.WithOpen(func() (flow.Operator, error) {
			return &skeletonDemuxerOperator{shape: demuxerShape}, nil
		})),
		plugin.NewComponent[skeletonParserID](plugin.Descriptor{DisplayName: "parser"}, configSchema, plugin.WithPorts(parserShape), plugin.WithOpen(func() (flow.Operator, error) {
			return &skeletonParserOperator{shape: parserShape}, nil
		})),
		plugin.NewComponent[skeletonCodecID](plugin.Descriptor{DisplayName: "decoder"}, configSchema, plugin.WithPorts(decoderShape), plugin.WithOpen(func() (flow.Operator, error) {
			return &skeletonCodecOperator{shape: decoderShape}, nil
		})),
		plugin.NewComponent[skeletonEncoderID](plugin.Descriptor{DisplayName: "encoder"}, configSchema, plugin.WithPorts(encoderShape), plugin.WithOpen(func() (flow.Operator, error) {
			return &skeletonEncoderOperator{shape: encoderShape}, nil
		})),
		plugin.NewComponent[skeletonMuxerID](plugin.Descriptor{DisplayName: "muxer"}, configSchema, plugin.WithPorts(muxerShape), plugin.WithOpen(func() (flow.Operator, error) {
			return &skeletonMuxerOperator{shape: muxerShape, trace: trace}, nil
		})),
	)
}

func skeletonBinding() codec.Binding {
	return codec.Bind(format.NewTag("fixture", "bytes"), codec.Define[skeletonCodecID](), codec.DefineParser[skeletonParserID]())
}

func openQueue[T any](descriptor schema.Descriptor) (schema.Queue[T], error) {
	erased, err := descriptor.NewPipe()
	if err != nil {
		return nil, err
	}
	queue, ok := erased.(schema.Queue[T])
	if !ok {
		return nil, fmt.Errorf("schema queue product has type %T", erased)
	}
	return queue, nil
}

func openFanout[T any](descriptor schema.Descriptor, outputs int) (schema.Fanout[T], error) {
	erased, err := descriptor.NewTee(outputs)
	if err != nil {
		return nil, err
	}
	fanout, ok := erased.(schema.Fanout[T])
	if !ok {
		return nil, fmt.Errorf("schema fan-out product has type %T", erased)
	}
	return fanout, nil
}

func TestWalkingSkeletonPreservesBytesTimingOrderAndOwnership(t *testing.T) {
	inputBytes := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	trace := &skeletonTrace{}
	definition := skeletonComponents(inputBytes, trace)
	trivialFormat, err := format.Define[skeletonFormatID]([]format.Alternative{format.AnyOf(format.SequentialRead)}, []format.Carrier{format.NewCarrier("fixture.payload", "format:fixture")})
	if err != nil || !trivialFormat.Valid() {
		t.Fatalf("trivial format = %#v, %v", trivialFormat, err)
	}
	instance, err := host.New(host.Plugins(plugin.NewSet(definition).AddDeclaration(skeletonBinding())))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sourceValue, err := instance.Open(plugin.IdentityOf[skeletonSourceID]())
	if err != nil {
		t.Fatal(err)
	}
	demuxerValue, err := instance.Open(plugin.IdentityOf[skeletonDemuxerID]())
	if err != nil {
		t.Fatal(err)
	}
	parserValue, err := instance.Open(plugin.IdentityOf[skeletonParserID]())
	if err != nil {
		t.Fatal(err)
	}
	decoderValue, err := instance.Open(plugin.IdentityOf[skeletonCodecID]())
	if err != nil {
		t.Fatal(err)
	}
	encoderValue, err := instance.Open(plugin.IdentityOf[skeletonEncoderID]())
	if err != nil {
		t.Fatal(err)
	}
	muxerValue, err := instance.Open(plugin.IdentityOf[skeletonMuxerID]())
	if err != nil {
		t.Fatal(err)
	}
	defer sourceValue.Close()
	defer demuxerValue.Close()
	defer parserValue.Close()
	defer decoderValue.Close()
	defer encoderValue.Close()
	defer muxerValue.Close()
	for _, operator := range []flow.Operator{sourceValue, demuxerValue, parserValue, decoderValue, encoderValue, muxerValue} {
		if err := operator.Ports().Validate(); err != nil {
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
	byteInput, err := source.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := demuxer.Process(ctx, byteInput, chunkOutput); err != nil {
		t.Fatal(err)
	}
	for _, chunk := range chunkOutput.items {
		packetOutput.items = packetOutput.items[:0]
		if err := parser.Process(ctx, chunk, packetOutput); err != nil {
			t.Fatal(err)
		}
		for _, packetValue := range packetOutput.items {
			frameOutput.items = frameOutput.items[:0]
			if err := decoder.Process(ctx, packetValue, frameOutput); err != nil {
				t.Fatal(err)
			}
			for _, frame := range frameOutput.items {
				encodedPacketOutput.items = encodedPacketOutput.items[:0]
				if err := encoder.Process(ctx, frame, encodedPacketOutput); err != nil {
					t.Fatal(err)
				}
				for _, encodedPacket := range encodedPacketOutput.items {
					if err := muxer.Process(ctx, encodedPacket, chunkEmitter); err != nil {
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
	for _, encodedPacket := range encodedPacketOutput.items {
		if err := muxer.Process(ctx, encodedPacket, chunkEmitter); err != nil {
			t.Fatal(err)
		}
	}
	if err := muxer.Flush(ctx, chunkEmitter); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Read(ctx); err != io.EOF {
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
	if video.Descriptor() != video.Descriptor() {
		t.Fatal("schema descriptor is not comparable")
	}
	queue, err := openQueue[videoUnit](shape.Inputs[0].Schema())
	if err != nil {
		t.Fatal(err)
	}
	if !queue.Push(videoUnit{Width: 128}) {
		t.Fatal("typed queue rejected an item")
	}
	if value, ok := queue.Pop(); !ok || value.Width != 128 {
		t.Fatalf("typed queue value = %#v, %v", value, ok)
	}
	fanout, err := openFanout[videoUnit](shape.Inputs[0].Schema(), 2)
	if err != nil {
		t.Fatal(err)
	}
	values := fanout.Split(videoUnit{Width: 1920})
	if len(values) != 2 || values[1].Width != 1920 {
		t.Fatalf("typed fan-out values = %#v", values)
	}
	for _, value := range values {
		fanout.Drop(value)
	}
}
