package media_test

import (
	"context"
	"fmt"
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
type skeletonBytesID struct{}
type skeletonChunkID struct{}
type skeletonPacketID struct{}
type skeletonFrameID struct{}
type skeletonConfig struct{ Value int }

var (
	skeletonBytesSchema  = schema.Define[skeletonBytesID, []byte](schema.Traits[[]byte]{})
	skeletonChunkSchema  = schema.Define[skeletonChunkID, packet.Chunk](schema.Traits[packet.Chunk]{})
	skeletonPacketSchema = schema.Define[skeletonPacketID, packet.Packet](schema.Traits[packet.Packet]{})
	skeletonFrameSchema  = schema.Define[skeletonFrameID, audio.Frame[int16]](schema.Traits[audio.Frame[int16]]{})
)

type skeletonSourceOperator struct{ shape flow.Shape }

func (o *skeletonSourceOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonSourceOperator) Close() error      { return nil }
func (o *skeletonSourceOperator) Convert(input flow.Input[[]byte]) ([]flow.Input[packet.Chunk], error) {
	owner := input.Take()
	if !owner.Valid() {
		return nil, fmt.Errorf("source input was not owned")
	}
	data := append([]byte(nil), owner.Value()...)
	owner.Release()
	const bytesPerChunk = 2
	result := make([]flow.Input[packet.Chunk], 0, (len(data)+bytesPerChunk-1)/bytesPerChunk)
	for offset, sequence := 0, uint64(0); offset < len(data); offset, sequence = offset+bytesPerChunk, sequence+1 {
		end := offset + bytesPerChunk
		if end > len(data) {
			end = len(data)
		}
		payload, err := buffer.FromBytes(data[offset:end], 8)
		if err != nil {
			for _, pending := range result {
				pending.Drop()
			}
			return nil, err
		}
		chunk := packet.NewTimestampedChunk(sequence, timing.SomePTS(timing.NewPTS(int64(sequence))), payload)
		result = append(result, flow.NewInput(chunk, func(value packet.Chunk) { value.Release() }))
	}
	return result, nil
}

type skeletonDecoderOperator struct{ shape flow.Shape }

func (o *skeletonDecoderOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonDecoderOperator) Close() error      { return nil }
func (o *skeletonDecoderOperator) Convert(input flow.Input[packet.Chunk]) (flow.Input[audio.Frame[int16]], error) {
	owner := input.Take()
	if !owner.Valid() {
		return flow.Input[audio.Frame[int16]]{}, fmt.Errorf("decoder input was not owned")
	}
	chunk := owner.Value()
	payload := chunk.Payload()
	frame, err := audio.NewFrame[int16](chunk.PTS(), len(chunk.Bytes())/2, payload)
	if err != nil {
		payload.Release()
		owner.Release()
		return flow.Input[audio.Frame[int16]]{}, err
	}
	owner.Release()
	return flow.NewInput(frame, func(value audio.Frame[int16]) { value.Release() }), nil
}

type skeletonEncoderOperator struct{ shape flow.Shape }

func (o *skeletonEncoderOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonEncoderOperator) Close() error      { return nil }
func (o *skeletonEncoderOperator) Convert(input flow.Input[audio.Frame[int16]]) (flow.Input[packet.Packet], error) {
	owner := input.Take()
	if !owner.Valid() {
		return flow.Input[packet.Packet]{}, fmt.Errorf("encoder input was not owned")
	}
	frame := owner.Value()
	payload := frame.Planes()
	value := packet.NewPacket(uint64(frame.PTS().Value()), frame.PTS(), timing.UnknownDTS(), timing.SomeDuration(timing.NewDuration(int64(frame.Samples()))), payload)
	owner.Release()
	return flow.NewInput(value, func(item packet.Packet) { item.Release() }), nil
}

type skeletonMuxerOperator struct{ shape flow.Shape }

func (o *skeletonMuxerOperator) Ports() flow.Shape { return o.shape }
func (o *skeletonMuxerOperator) Close() error      { return nil }
func (o *skeletonMuxerOperator) Convert(input flow.Input[packet.Packet]) (flow.Input[packet.Chunk], error) {
	owner := input.Take()
	if !owner.Valid() {
		return flow.Input[packet.Chunk]{}, fmt.Errorf("muxer input was not owned")
	}
	value := owner.Value()
	payload := value.Payload()
	chunk := packet.NewTimestampedChunk(value.Sequence(), value.PTS(), payload)
	owner.Release()
	return flow.NewInput(chunk, func(item packet.Chunk) { item.Release() }), nil
}

func skeletonConfigSchema() config.Schema[skeletonConfig] {
	return config.Struct(func() skeletonConfig { return skeletonConfig{} }).
		Identity("media.foundation.skeleton").
		Version("1").
		AddField(config.Field("value", func(value *skeletonConfig) *int { return &value.Value }, config.Int())).
		Build()
}

func skeletonComponents() plugin.Definition {
	sourceShape := flow.NewShape([]flow.Port{flow.In("bytes", skeletonBytesSchema)}, []flow.Port{flow.Out("chunks", skeletonChunkSchema, flow.Many())})
	decoderShape := flow.NewShape([]flow.Port{flow.In("chunks", skeletonChunkSchema)}, []flow.Port{flow.Out("frames", skeletonFrameSchema)})
	encoderShape := flow.NewShape([]flow.Port{flow.In("frames", skeletonFrameSchema)}, []flow.Port{flow.Out("packets", skeletonPacketSchema)})
	muxerShape := flow.NewShape([]flow.Port{flow.In("packets", skeletonPacketSchema)}, []flow.Port{flow.Out("chunks", skeletonChunkSchema)})
	configSchema := skeletonConfigSchema()
	return plugin.Define[skeletonPluginID](plugin.Descriptor{DisplayName: "foundation skeleton", Version: "1"},
		plugin.NewComponent[skeletonSourceID](plugin.Descriptor{DisplayName: "source"}, configSchema, plugin.WithPorts(sourceShape), plugin.WithOpen(func() (flow.Operator, error) { return &skeletonSourceOperator{shape: sourceShape}, nil })),
		plugin.NewComponent[skeletonDecoderID](plugin.Descriptor{DisplayName: "decoder"}, configSchema, plugin.WithPorts(decoderShape), plugin.WithOpen(func() (flow.Operator, error) { return &skeletonDecoderOperator{shape: decoderShape}, nil })),
		plugin.NewComponent[skeletonEncoderID](plugin.Descriptor{DisplayName: "encoder"}, configSchema, plugin.WithPorts(encoderShape), plugin.WithOpen(func() (flow.Operator, error) { return &skeletonEncoderOperator{shape: encoderShape}, nil })),
		plugin.NewComponent[skeletonMuxerID](plugin.Descriptor{DisplayName: "muxer"}, configSchema, plugin.WithPorts(muxerShape), plugin.WithOpen(func() (flow.Operator, error) { return &skeletonMuxerOperator{shape: muxerShape}, nil })),
	)
}

func skeletonBinding() codec.Binding {
	parser := codec.NewParser("fixture:parser", func(context.Context, packet.Chunk) ([]packet.Packet, error) { return nil, nil })
	return codec.Bind(format.NewTag("fixture", "bytes"), codec.New("fixture:codec"), parser)
}

func TestWalkingSkeletonPreservesBytesTimingOrderAndOwnership(t *testing.T) {
	definition := skeletonComponents()
	trivialFormat, err := format.New(format.NewTag("fixture", "raw"), []format.Alternative{format.AnyOf(format.SequentialRead)}, []format.Carrier{format.NewCarrier("fixture.payload", "format:fixture")})
	if err != nil || !trivialFormat.Valid() {
		t.Fatalf("trivial format = %#v, %v", trivialFormat, err)
	}
	baseSet := plugin.NewSet(definition).AddBinding(skeletonBinding())
	instance, err := host.New(host.Plugins(baseSet))
	if err != nil {
		t.Fatal(err)
	}
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
	source := sourceValue.(*skeletonSourceOperator)
	decoder := decoderValue.(*skeletonDecoderOperator)
	encoder := encoderValue.(*skeletonEncoderOperator)
	muxer := muxerValue.(*skeletonMuxerOperator)
	defer source.Close()
	defer decoder.Close()
	defer encoder.Close()
	defer muxer.Close()
	for _, operator := range []flow.Operator{source, decoder, encoder, muxer} {
		if err := operator.Ports().Validate(); err != nil {
			t.Fatal(err)
		}
	}

	inputBytes := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	byteInput := flow.NewInput(inputBytes, nil)
	chunks, err := source.Convert(byteInput)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 4 {
		t.Fatalf("chunk count = %d", len(chunks))
	}
	outputBytes := make([]byte, 0, len(inputBytes))
	var sequences []uint64
	var timestamps []timing.PTS
	for _, chunkInput := range chunks {
		frameInput, err := decoder.Convert(chunkInput)
		if err != nil {
			t.Fatal(err)
		}
		packetInput, err := encoder.Convert(frameInput)
		if err != nil {
			t.Fatal(err)
		}
		outputInput, err := muxer.Convert(packetInput)
		if err != nil {
			t.Fatal(err)
		}
		chunkOwner := outputInput.Take()
		if !chunkOwner.Valid() {
			t.Fatal("output chunk ownership was not transferred")
		}
		value := chunkOwner.Value()
		sequences = append(sequences, value.Sequence())
		timestamps = append(timestamps, value.PTS().Value())
		outputBytes = append(outputBytes, value.Bytes()...)
		chunkOwner.Release()
	}
	if string(outputBytes) != string(inputBytes) {
		t.Fatalf("round trip bytes = %v, want %v", outputBytes, inputBytes)
	}
	for index := range sequences {
		if sequences[index] != uint64(index) || timestamps[index] != timing.PTS(index) {
			t.Fatalf("item %d = sequence %d, pts %d", index, sequences[index], timestamps[index])
		}
	}
}

func TestWalkingSkeletonRejectsConflictingBindingInHostBuild(t *testing.T) {
	base := plugin.NewSet(skeletonComponents()).AddBinding(skeletonBinding())
	conflict := codec.BindWithoutParser(format.NewTag("fixture", "bytes"), codec.New("fixture:other-codec"))
	if _, err := host.New(host.Plugins(base.AddBinding(conflict))); err == nil {
		t.Fatal("host accepted conflicting codec binding")
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
