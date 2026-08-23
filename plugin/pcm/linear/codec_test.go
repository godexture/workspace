package linear

import (
	"context"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/timing"
)

type frameSink[S audio.Sample] struct{ frames []audio.Frame[S] }

func (*frameSink[S]) Own(into *flow.Item[audio.Frame[S]], value audio.Frame[S]) {
	into.Bind(sample.Frames[S](), &testDomain)
	into.Set(value)
}

func (s *frameSink[S]) Emit(_ context.Context, item *flow.Item[audio.Frame[S]]) error {
	s.frames = append(s.frames, item.Value().Share())
	item.Drop()
	return nil
}

type collectingSink struct{ payloads [][]byte }

func (*collectingSink) Own(into *flow.Item[packet.Packet], value packet.Packet) {
	into.Bind(codec.Packets(), &testDomain)
	into.Set(value)
}

func (s *collectingSink) Emit(_ context.Context, item *flow.Item[packet.Packet]) error {
	s.payloads = append(s.payloads, item.Value().Bytes().AppendTo(nil))
	item.Drop()
	return nil
}

func wireDescription(coding sample.Coding, endian sample.Endian, layout sample.Layout) sample.Description {
	if coding.Bytes() == 1 {
		endian = sample.NoEndian
	}
	return sample.Description{
		Coding: coding, Packing: sample.Interleaved, Endian: endian,
		Rate: 48_000, Layout: layout, ValidBits: coding.Bits(),
	}
}

// A decoded sample spans the full scale of its canonical coding, so the wire
// bytes below translate the same extremes into every representation. A coding
// narrower than its container is left-aligned exactly as the container formats
// that carry it define, which is what keeps a filter free of the wire shape.
func TestDecodeAndEncodeCoverEveryCoding(t *testing.T) {
	t.Run("s16", func(t *testing.T) {
		codingRoundTrip(t, wireDescription(sample.U8, sample.NoEndian, sample.Stereo()),
			[]byte{0x00, 0xff, 0x80, 0x81},
			[][]int16{{-32768, 0}, {32512, 256}})
		codingRoundTrip(t, wireDescription(sample.S8, sample.NoEndian, sample.Mono()),
			[]byte{0x80, 0x7f, 0x00, 0xff},
			[][]int16{{-32768, 32512, 0, -256}})
		codingRoundTrip(t, wireDescription(sample.S16, sample.LittleEndian, sample.Mono()),
			[]byte{0x00, 0x80, 0xff, 0x7f},
			[][]int16{{-32768, 32767}})
		codingRoundTrip(t, wireDescription(sample.S16, sample.BigEndian, sample.Stereo()),
			[]byte{0x80, 0x00, 0x7f, 0xff, 0x00, 0x01, 0xff, 0xff},
			[][]int16{{-32768, 1}, {32767, -1}})
	})

	t.Run("s32", func(t *testing.T) {
		codingRoundTrip(t, wireDescription(sample.S24, sample.LittleEndian, sample.Mono()),
			[]byte{0x00, 0x00, 0x80, 0xff, 0xff, 0x7f},
			[][]int32{{-2147483648, 2147483392}})
		codingRoundTrip(t, wireDescription(sample.S24, sample.BigEndian, sample.Stereo()),
			[]byte{0x80, 0x00, 0x00, 0x7f, 0xff, 0xff, 0x00, 0x00, 0x01, 0xff, 0xff, 0xff},
			[][]int32{{-2147483648, 256}, {2147483392, -256}})
		codingRoundTrip(t, wireDescription(sample.S32, sample.LittleEndian, sample.Stereo()),
			[]byte{0x00, 0x00, 0x00, 0x80, 0xff, 0xff, 0xff, 0x7f},
			[][]int32{{-2147483648}, {2147483647}})
		codingRoundTrip(t, wireDescription(sample.S32, sample.BigEndian, sample.Mono()),
			[]byte{0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
			[][]int32{{-2147483648, 1}})
	})

	t.Run("float", func(t *testing.T) {
		codingRoundTrip(t, wireDescription(sample.F32, sample.LittleEndian, sample.Mono()),
			[]byte{0x00, 0x00, 0x80, 0xbf, 0x00, 0x00, 0x80, 0x3f},
			[][]float32{{-1, 1}})
		codingRoundTrip(t, wireDescription(sample.F32, sample.BigEndian, sample.Mono()),
			[]byte{0xbf, 0x80, 0x00, 0x00, 0x3f, 0x80, 0x00, 0x00},
			[][]float32{{-1, 1}})
		codingRoundTrip(t, wireDescription(sample.F64, sample.BigEndian, sample.Stereo()),
			[]byte{
				0xbf, 0xf0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x3f, 0xf0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			[][]float64{{-1}, {1}})
	})
}

// Positions the vocabulary does not name still describe a stream, so a layout
// that only states its channel count decodes like a named one.
func TestDecodeAndEncodeCoverMoreThanTwoChannels(t *testing.T) {
	codingRoundTrip(t, wireDescription(sample.S16, sample.LittleEndian, sample.Channels(3)),
		[]byte{0x01, 0x00, 0x02, 0x00, 0x03, 0x00, 0x04, 0x00, 0x05, 0x00, 0x06, 0x00},
		[][]int16{{1, 4}, {2, 5}, {3, 6}})

	surround := sample.Positions(sample.FrontLeft, sample.FrontRight, sample.FrontCenter,
		sample.LowFrequency, sample.BackLeft, sample.BackRight)
	encoded := make([]byte, 6*3*2)
	want := make([][]int32, 6)
	for channel := range want {
		want[channel] = make([]int32, 2)
		for position := range want[channel] {
			value := int32(channel+1) << 16 * int32(position+1)
			want[channel][position] = value
			offset := (position*6 + channel) * 3
			encoded[offset] = byte(uint32(value) >> 8)
			encoded[offset+1] = byte(uint32(value) >> 16)
			encoded[offset+2] = byte(uint32(value) >> 24)
		}
	}
	codingRoundTrip(t, wireDescription(sample.S24, sample.LittleEndian, surround), encoded, want)
}

// A payload longer than the operator scratch is drained in several blocks. The
// per-channel offsets have to survive that split, which a single-block fixture
// never exercises.
func TestDecodeCrossesTheScratchBoundary(t *testing.T) {
	description := wireDescription(sample.S16, sample.LittleEndian, sample.Stereo())
	samples := blockWords*8/description.BlockBytes() + 17
	encoded := make([]byte, samples*description.BlockBytes())
	want := [][]int16{make([]int16, samples), make([]int16, samples)}
	for index := range samples {
		for channel := range want {
			value := int16((index*37 + channel*101) % 30011)
			want[channel][index] = value
			offset := (index*2 + channel) * 2
			encoded[offset], encoded[offset+1] = byte(uint16(value)), byte(uint16(value)>>8)
		}
	}
	codingRoundTrip(t, description, encoded, want)
}

func codingRoundTrip[S audio.Sample](t *testing.T, description sample.Description, encoded []byte, want [][]S) {
	t.Helper()
	if !description.Valid() {
		t.Fatalf("%#v is not a valid description", description)
	}
	allocator, err := buffer.NewAllocator(int64(len(encoded))*8 + 1024)
	if err != nil {
		t.Fatal(err)
	}
	frame := decodeOnce[S](t, allocator, description, encoded, len(want[0]))
	for channel := range want {
		values, err := frame.PlaneSamples(channel)
		if err != nil {
			t.Fatal(err)
		}
		got := values.AppendTo(nil)
		if len(got) != len(want[channel]) {
			t.Fatalf("%s channel %d length = %d, want %d", description.Coding, channel, len(got), len(want[channel]))
		}
		for index := range got {
			if got[index] != want[channel][index] {
				t.Fatalf("%s channel %d sample %d = %v, want %v", description.Coding, channel, index, got[index], want[channel][index])
			}
		}
	}
	got := encodeOnce[S](t, allocator, description, frame)
	frame.Release()
	if string(got) != string(encoded) {
		t.Fatalf("%s %s round trip = %x, want %x", description.Coding, description.Endian, got, encoded)
	}
	if used := allocator.Used(); used != 0 {
		t.Fatalf("%s retained %d bytes", description.Coding, used)
	}
}

func decodeOnce[S audio.Sample](t *testing.T, allocator *buffer.Allocator, description sample.Description, encoded []byte, samples int) audio.Frame[S] {
	t.Helper()
	operator, err := openCodec[S](codecPlan(decoderOperation, description, samples), allocator)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := allocator.FromBytes(encoded, 16)
	if err != nil {
		t.Fatal(err)
	}
	var item flow.Item[packet.Packet]
	item.Bind(codec.Packets(), &testDomain)
	item.Set(packet.NewPacket(0, timing.SomePTS(timing.NewPTS(0)), timing.UnknownDTS(), timing.UnknownDuration(), payload))
	sink := &frameSink[S]{}
	if err := operator.(*decoderOperator[S]).Process(t.Context(), &item, sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.frames) != 1 {
		t.Fatalf("decoded frames = %d", len(sink.frames))
	}
	return sink.frames[0]
}

func encodeOnce[S audio.Sample](t *testing.T, allocator *buffer.Allocator, description sample.Description, frame audio.Frame[S]) []byte {
	t.Helper()
	operator, err := openCodec[S](codecPlan(encoderOperation, description, frame.Samples()), allocator)
	if err != nil {
		t.Fatal(err)
	}
	var item flow.Item[audio.Frame[S]]
	item.Bind(sample.Frames[S](), &testDomain)
	item.Set(frame.Share())
	sink := &collectingSink{}
	if err := operator.(*encoderOperator[S]).Process(t.Context(), &item, sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.payloads) != 1 {
		t.Fatalf("encoded packets = %d", len(sink.payloads))
	}
	return sink.payloads[0]
}

func codecPlan(kind operation, description sample.Description, samples int) componentPlan {
	return componentPlan{
		operation: kind,
		config: configuration{
			Coding: description.Coding, Endian: description.Endian, Rate: description.Rate,
			Layout: description.Layout, ValidBits: description.ValidBits, ChunkSamples: max(samples, 1),
		},
		wire:   description,
		frames: description.Decoded(),
	}
}

func TestCodecComponentsRejectCodingsTheyCannotStore(t *testing.T) {
	description := wireDescription(sample.S24, sample.LittleEndian, sample.Mono())
	if _, _, err := newUnpack[int16](description); err == nil {
		t.Error("a 24-bit wire unpacked into s16 frames")
	}
	if _, _, err := newPack[float32](description); err == nil {
		t.Error("a 24-bit wire packed from f32 frames")
	}
	if _, _, err := newUnpack[int32](description); err != nil {
		t.Errorf("a 24-bit wire did not unpack into s32 frames: %v", err)
	}
}
