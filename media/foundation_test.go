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
type skeletonDecoderID struct{}
type skeletonEncoderID struct{}
type skeletonMuxerID struct{}
type skeletonCodecID struct{}
type skeletonParserID struct{}
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

type skeletonDecoderOperator struct{ shape flow.Shape }

func (o *skeletonDecoderOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonDecoderOperator) Close() error      { return nil }

func (o *skeletonDecoderOperator) Process(ctx context.Context, input flow.Input[[]byte], output flow.Emitter[packet.Chunk]) error {
	owner := input.Take()
	if !owner.Valid() {
		return fmt.Errorf("decoder input was not owned")
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

func (o *skeletonDecoderOperator) Flush(context.Context, flow.Emitter[packet.Chunk]) error {
	return nil
}

type skeletonEncoderOperator struct {
	shape      flow.Shape
	pending    audio.Frame[int16]
	hasPending bool
}

func (o *skeletonEncoderOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonEncoderOperator) Close() error      { return nil }

func (o *skeletonEncoderOperator) Process(ctx context.Context, input flow.Input[packet.Chunk], output flow.Emitter[audio.Frame[int16]]) error {
	owner := input.Take()
	if !owner.Valid() {
		return fmt.Errorf("encoder input was not owned")
	}
	chunk := owner.Value()
	payload := chunk.Payload().Share()
	frame, err := audio.NewFrame[int16](chunk.PTS(), len(chunk.Bytes())/2, payload)
	if err != nil {
		payload.Release()
		owner.Release()
		return err
	}
	owner.Release()
	if o.hasPending {
		item := flow.NewInput(o.pending, skeletonFrameSchema)
		o.hasPending = false
		if err := output.Emit(ctx, item); err != nil {
			item.Drop()
			return err
		}
	}
	o.pending = frame
	o.hasPending = true
	return nil
}

func (o *skeletonEncoderOperator) Flush(ctx context.Context, output flow.Emitter[audio.Frame[int16]]) error {
	if !o.hasPending {
		return nil
	}
	item := flow.NewInput(o.pending, skeletonFrameSchema)
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
	shape *flow.Shape
	trace *skeletonTrace
}

func (o *skeletonMuxerOperator) Ports() flow.Shape {
	if o.shape == nil {
		return flow.Shape{}
	}
	return o.shape.Clone()
}

func (o *skeletonMuxerOperator) Close() error { return nil }

func (o *skeletonMuxerOperator) Process(ctx context.Context, input flow.Input[audio.Frame[int16]], output flow.Emitter[[]byte]) error {
	owner := input.Take()
	if !owner.Valid() {
		return fmt.Errorf("muxer input was not owned")
	}
	frame := owner.Value()
	payload := frame.Planes().Share()
	packetValue := packet.NewPacket(uint64(frame.PTS().Value()), frame.PTS(), timing.UnknownDTS(), timing.SomeDuration(timing.NewDuration(int64(frame.Samples()))), payload)
	chunkPayload := packetValue.Payload().Share()
	chunk := packet.NewTimestampedChunk(packetValue.Sequence(), packetValue.PTS(), chunkPayload)
	value := append([]byte(nil), chunk.Bytes()...)
	if o.trace != nil {
		o.trace.sequences = append(o.trace.sequences, chunk.Sequence())
		o.trace.timestamps = append(o.trace.timestamps, chunk.PTS().Value())
	}
	chunk.Release()
	packetValue.Release()
	owner.Release()
	item := flow.NewInput(value, skeletonBytesSchema)
	if err := output.Emit(ctx, item); err != nil {
		item.Drop()
		return err
	}
	return nil
}

func (o *skeletonMuxerOperator) Flush(context.Context, flow.Emitter[[]byte]) error {
	return nil
}

type skeletonDeclarationOperator struct{ shape flow.Shape }

func (o *skeletonDeclarationOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonDeclarationOperator) Close() error      { return nil }

type skeletonByteWriter struct {
	bytes []byte
	items int
}

func (w *skeletonByteWriter) Write(_ context.Context, input flow.Input[[]byte]) error {
	owner := input.Take()
	if !owner.Valid() {
		return fmt.Errorf("writer input was not owned")
	}
	w.bytes = append(w.bytes, owner.Value()...)
	w.items++
	owner.Release()
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
	decoderShape := flow.NewShape([]flow.Port{flow.In("bytes", skeletonBytesSchema)}, []flow.Port{flow.Out("chunks", skeletonChunkSchema, flow.Many())})
	encoderShape := flow.NewShape([]flow.Port{flow.In("chunks", skeletonChunkSchema)}, []flow.Port{flow.Out("frames", skeletonFrameSchema)})
	muxerShape := flow.NewShape([]flow.Port{flow.In("frames", skeletonFrameSchema)}, []flow.Port{flow.Out("bytes", skeletonBytesSchema)})
	declarationShape := flow.NewShape([]flow.Port{flow.In("input", skeletonPacketSchema)}, []flow.Port{flow.Out("output", skeletonPacketSchema)})
	configSchema := skeletonConfigSchema()
	return plugin.Define[skeletonPluginID](plugin.Descriptor{DisplayName: "foundation skeleton", Version: "1"},
		plugin.NewComponent[skeletonSourceID](plugin.Descriptor{DisplayName: "source"}, configSchema, plugin.WithPorts(sourceShape), plugin.WithOpen(func() (flow.Operator, error) {
			return &skeletonSourceOperator{shape: sourceShape, data: append([]byte(nil), data...)}, nil
		})),
		plugin.NewComponent[skeletonDecoderID](plugin.Descriptor{DisplayName: "decoder"}, configSchema, plugin.WithPorts(decoderShape), plugin.WithOpen(func() (flow.Operator, error) {
			return &skeletonDecoderOperator{shape: decoderShape}, nil
		})),
		plugin.NewComponent[skeletonEncoderID](plugin.Descriptor{DisplayName: "encoder"}, configSchema, plugin.WithPorts(encoderShape), plugin.WithOpen(func() (flow.Operator, error) {
			return &skeletonEncoderOperator{shape: encoderShape}, nil
		})),
		plugin.NewComponent[skeletonMuxerID](plugin.Descriptor{DisplayName: "muxer"}, configSchema, plugin.WithPorts(muxerShape), plugin.WithOpen(func() (flow.Operator, error) {
			return &skeletonMuxerOperator{shape: &muxerShape, trace: trace}, nil
		})),
		plugin.NewComponent[skeletonCodecID](plugin.Descriptor{DisplayName: "codec"}, configSchema, plugin.WithPorts(declarationShape), plugin.WithOpen(func() (flow.Operator, error) {
			return &skeletonDeclarationOperator{shape: declarationShape}, nil
		})),
		plugin.NewComponent[skeletonParserID](plugin.Descriptor{DisplayName: "parser"}, configSchema, plugin.WithPorts(declarationShape), plugin.WithOpen(func() (flow.Operator, error) {
			return &skeletonDeclarationOperator{shape: declarationShape}, nil
		})),
	)
}

func skeletonBinding() codec.Binding {
	return codec.Bind(format.NewTag("fixture", "bytes"), codec.Define[skeletonCodecID](), codec.DefineParser[skeletonParserID]())
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
	decoderValue, err := instance.Open(plugin.IdentityOf[skeletonDecoderID]())
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
	defer decoderValue.Close()
	defer encoderValue.Close()
	defer muxerValue.Close()
	for _, operator := range []flow.Operator{sourceValue, decoderValue, encoderValue, muxerValue} {
		if err := operator.Ports().Validate(); err != nil {
			t.Fatal(err)
		}
	}
	source, ok := sourceValue.(flow.Reader[[]byte])
	if !ok {
		t.Fatal("source did not implement flow.Reader[[]byte]")
	}
	decoder, ok := decoderValue.(flow.Processor[[]byte, packet.Chunk])
	if !ok {
		t.Fatal("decoder did not implement flow.Processor[[]byte, packet.Chunk]")
	}
	encoder, ok := encoderValue.(flow.Processor[packet.Chunk, audio.Frame[int16]])
	if !ok {
		t.Fatal("encoder did not implement flow.Processor[packet.Chunk, audio.Frame[int16]]")
	}
	muxer, ok := muxerValue.(flow.Processor[audio.Frame[int16], []byte])
	if !ok {
		t.Fatal("muxer did not implement flow.Processor[audio.Frame[int16], []byte]")
	}
	writer := &skeletonByteWriter{}
	var sink flow.Writer[[]byte] = writer
	emitter := skeletonWriterEmitter[[]byte]{sink: sink}
	chunkOutput := &skeletonEmitter[packet.Chunk]{}
	frameOutput := &skeletonEmitter[audio.Frame[int16]]{}
	byteInput, err := source.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := decoder.Process(ctx, byteInput, chunkOutput); err != nil {
		t.Fatal(err)
	}
	for _, chunk := range chunkOutput.items {
		frameOutput.items = frameOutput.items[:0]
		if err := encoder.Process(ctx, chunk, frameOutput); err != nil {
			t.Fatal(err)
		}
		for _, frame := range frameOutput.items {
			if err := muxer.Process(ctx, frame, emitter); err != nil {
				t.Fatal(err)
			}
		}
	}
	frameOutput.items = frameOutput.items[:0]
	if err := encoder.Flush(ctx, frameOutput); err != nil {
		t.Fatal(err)
	}
	for _, frame := range frameOutput.items {
		if err := muxer.Process(ctx, frame, emitter); err != nil {
			t.Fatal(err)
		}
	}
	if err := muxer.Flush(ctx, emitter); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Read(ctx); err != io.EOF {
		t.Fatalf("second source read = %v, want EOF", err)
	}
	if string(writer.bytes) != string(inputBytes) {
		t.Fatalf("round trip bytes = %v, want %v", writer.bytes, inputBytes)
	}
	if writer.items != 4 || len(trace.sequences) != 4 {
		t.Fatalf("output items = %d, trace = %d, want 4", writer.items, len(trace.sequences))
	}
	for index := range trace.sequences {
		if trace.sequences[index] != uint64(index) || trace.timestamps[index] != timing.PTS(index) {
			t.Fatalf("item %d = sequence %d, pts %d", index, trace.sequences[index], trace.timestamps[index])
		}
	}
}

func TestWalkingSkeletonRejectsConflictingBindingInHostBuild(t *testing.T) {
	base := plugin.NewSet(skeletonComponents(nil, nil)).AddDeclaration(skeletonBinding())
	conflict := codec.BindWithoutParser(format.NewTag("fixture", "bytes"), codec.New(plugin.IdentityOf[skeletonDecoderID]()))
	if _, err := host.New(host.Plugins(base.AddDeclaration(conflict))); err == nil {
		t.Fatal("host accepted conflicting codec declaration")
	}
}

func TestThirdPartyNonAudioSchemasConnectWithoutCoreChanges(t *testing.T) {
	type videoID struct{}
	type videoUnit struct{ Width int }
	type subtitleID struct{}
	type subtitleCue struct{ Text string }
	video := schema.Define[videoID, videoUnit](schema.Traits[videoUnit]{})
	subtitle := schema.Define[subtitleID, subtitleCue](schema.Traits[subtitleCue]{})
	shape := flow.NewShape([]flow.Port{flow.In("video", video)}, []flow.Port{flow.Out("subtitle", subtitle)})
	if err := shape.Validate(); err != nil {
		t.Fatal(err)
	}
	if video.Identity() == subtitle.Identity() || video.Identity().IsZero() || subtitle.Identity().IsZero() {
		t.Fatal("third-party schema identities are not open and distinct")
	}
}
