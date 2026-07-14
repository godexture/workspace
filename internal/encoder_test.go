package internal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/hash"
)

func TestEncoder_ReceivePacketEmptyActive(t *testing.T) {
	encoder := NewEncoder(DefaultEncoderConfig())
	pkt, err := encoder.ReceivePacket()
	if !errors.Is(err, engine.ErrEAGAIN) || pkt != nil {
		t.Fatalf("expected ErrEAGAIN and nil packet, got err=%v, packet=%v", err, pkt)
	}
}

func TestEncoder_ReceivePacketEmptyFlushed(t *testing.T) {
	encoder := NewEncoder(DefaultEncoderConfig())
	if err := encoder.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	pkt, err := encoder.ReceivePacket()
	if !errors.Is(err, engine.ErrEOF) || pkt != nil {
		t.Fatalf("expected ErrEOF and nil packet, got err=%v, packet=%v", err, pkt)
	}
}

func TestEncoder_SendFrameAfterFlush(t *testing.T) {
	encoder := NewEncoder(DefaultEncoderConfig())
	if err := encoder.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	frame := makeAudioFrameS16(t, media.LayoutMono1, 44100, 0, []int16{1})
	var wrapped media.Frame = frame
	if err := encoder.SendFrame(&wrapped); !errors.Is(err, engine.ErrEOF) {
		t.Fatalf("expected ErrEOF after flush, got %v", err)
	}
}

func TestEncoder_SendNilFrame(t *testing.T) {
	encoder := NewEncoder(DefaultEncoderConfig())
	if err := encoder.SendFrame(nil); err == nil {
		t.Fatal("expected error for nil frame")
	}
}

func TestEncoder_S16StereoRoundtrip(t *testing.T) {
	encoder := NewEncoder(EncoderConfig{BlockSize: 4})
	input := []int16{0, 100, 1, 99, 2, 98, 3, 97}
	frame := makeAudioFrameS16(t, media.LayoutStereo2_0, 44100, 42, input)
	var wrapped media.Frame = frame
	if err := encoder.SendFrame(&wrapped); err != nil {
		t.Fatalf("SendFrame() error = %v", err)
	}

	pkt, err := encoder.ReceivePacket()
	if err != nil {
		t.Fatalf("ReceivePacket() error = %v", err)
	}
	if pkt.PTS != 42 || pkt.DTS != 42 {
		t.Fatalf("packet timestamps = (%d, %d), want (42, 42)", pkt.PTS, pkt.DTS)
	}

	decoded := decodePacketSamples(t, pkt, streamInfoFor(4, 44100, 2, 16))
	want := [][]int64{{0, 1, 2, 3}, {100, 99, 98, 97}}
	assertSamplesEqual(t, decoded, want)
}

func TestEncoder_FlushEmitsFinalPartialBlock(t *testing.T) {
	encoder := NewEncoder(EncoderConfig{BlockSize: 4})
	frame := makeAudioFrameS16(t, media.LayoutMono1, 44100, 7, []int16{1, 2, 3})
	var wrapped media.Frame = frame
	if err := encoder.SendFrame(&wrapped); err != nil {
		t.Fatalf("SendFrame() error = %v", err)
	}
	if pkt, err := encoder.ReceivePacket(); !errors.Is(err, engine.ErrEAGAIN) || pkt != nil {
		t.Fatalf("ReceivePacket() before flush = (%v, %v), want (nil, ErrEAGAIN)", pkt, err)
	}

	if err := encoder.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	pkt, err := encoder.ReceivePacket()
	if err != nil {
		t.Fatalf("ReceivePacket() after flush error = %v", err)
	}
	decoded := decodePacketSamples(t, pkt, streamInfoFor(3, 44100, 1, 16))
	assertSamplesEqual(t, decoded, [][]int64{{1, 2, 3}})
	if pkt, err := encoder.ReceivePacket(); !errors.Is(err, engine.ErrEOF) || pkt != nil {
		t.Fatalf("ReceivePacket() after final packet = (%v, %v), want (nil, ErrEOF)", pkt, err)
	}
}

func TestEncoder_S32As24BitRoundtrip(t *testing.T) {
	encoder := NewEncoder(EncoderConfig{BlockSize: 4, BitsPerSample: 24})
	input := []int32{-8_388_608, -1, 0, 8_388_607}
	frame := makeAudioFrameS32(t, media.LayoutMono1, 48000, 0, 24, input)
	var wrapped media.Frame = frame
	if err := encoder.SendFrame(&wrapped); err != nil {
		t.Fatalf("SendFrame() error = %v", err)
	}

	pkt, err := encoder.ReceivePacket()
	if err != nil {
		t.Fatalf("ReceivePacket() error = %v", err)
	}
	decoded := decodePacketSamples(t, pkt, streamInfoFor(4, 48000, 1, 24))
	assertSamplesEqual(t, decoded, [][]int64{{-8_388_608, -1, 0, 8_388_607}})
}

func TestEncoder_S32As32BitRoundtrip(t *testing.T) {
	encoder := NewEncoder(EncoderConfig{BlockSize: 4, BitsPerSample: 32})
	input := []int32{-2_147_483_648, -1, 0, 2_147_483_647}
	frame := makeAudioFrameS32(t, media.LayoutMono1, 96000, 0, 32, input)
	var wrapped media.Frame = frame
	if err := encoder.SendFrame(&wrapped); err != nil {
		t.Fatalf("SendFrame() error = %v", err)
	}

	pkt, err := encoder.ReceivePacket()
	if err != nil {
		t.Fatalf("ReceivePacket() error = %v", err)
	}
	decoded := decodePacketSamples(t, pkt, streamInfoFor(4, 96000, 1, 32))
	assertSamplesEqual(t, decoded, [][]int64{{-2_147_483_648, -1, 0, 2_147_483_647}})
}

func TestEncoder_Rejects24BitOutOfRange(t *testing.T) {
	encoder := NewEncoder(EncoderConfig{BlockSize: 1, BitsPerSample: 24})
	frame := makeAudioFrameS32(t, media.LayoutMono1, 44100, 0, 24, []int32{8_388_608})
	var wrapped media.Frame = frame
	if err := encoder.SendFrame(&wrapped); err == nil {
		t.Fatal("expected out-of-range S32/24-bit error")
	}
}

func TestEncoder_RejectsStreamChange(t *testing.T) {
	encoder := NewEncoder(EncoderConfig{BlockSize: 16})
	first := makeAudioFrameS16(t, media.LayoutMono1, 44100, 0, []int16{1})
	var firstWrapped media.Frame = first
	if err := encoder.SendFrame(&firstWrapped); err != nil {
		t.Fatalf("SendFrame(first) error = %v", err)
	}

	second := makeAudioFrameS16(t, media.LayoutMono1, 48000, 1, []int16{2})
	var secondWrapped media.Frame = second
	if err := encoder.SendFrame(&secondWrapped); err == nil {
		t.Fatal("expected stream change error")
	}
}

func TestEncodeFLACFrame_ConstantSubframeRoundtrip(t *testing.T) {
	data, err := encodeFLACFrame([][]int64{{123, 123, 123, 123}}, 44100, 16, 0, 4)
	if err != nil {
		t.Fatalf("encodeFLACFrame() error = %v", err)
	}
	decoded, err := decodeFLACFrame(data, streamInfoFor(4, 44100, 1, 16))
	if err != nil {
		t.Fatalf("decodeFLACFrame() error = %v", err)
	}
	assertSamplesEqual(t, decoded.samples, [][]int64{{123, 123, 123, 123}})
}

func TestEncodeFLACFrame_FixedSubframeRoundtrip(t *testing.T) {
	data, err := encodeFLACFrame([][]int64{{0, 1, 2, 3, 4, 5, 6, 7}}, 44100, 16, 12, 4)
	if err != nil {
		t.Fatalf("encodeFLACFrame() error = %v", err)
	}
	decoded, err := decodeFLACFrame(data, streamInfoFor(8, 44100, 1, 16))
	if err != nil {
		t.Fatalf("decodeFLACFrame() error = %v", err)
	}
	if decoded.header.number != 12 {
		t.Fatalf("frame number = %d, want 12", decoded.header.number)
	}
	assertSamplesEqual(t, decoded.samples, [][]int64{{0, 1, 2, 3, 4, 5, 6, 7}})
}

func TestEncodeFLACFrame_VerbatimFallbackRoundtrip(t *testing.T) {
	// Values outside the FLAC residual range cannot be fixed/Rice-coded, so the
	// encoder must fall back to verbatim while still preserving exact samples.
	data, err := encodeFLACFrame([][]int64{{-2_147_483_648, 0, 2_147_483_647}}, 44100, 32, 0, 4)
	if err != nil {
		t.Fatalf("encodeFLACFrame() error = %v", err)
	}
	decoded, err := decodeFLACFrame(data, streamInfoFor(3, 44100, 1, 32))
	if err != nil {
		t.Fatalf("decodeFLACFrame() error = %v", err)
	}
	assertSamplesEqual(t, decoded.samples, [][]int64{{-2_147_483_648, 0, 2_147_483_647}})
}

func TestEncodeFLACFrame_FullBitstreamFeaturesRoundtrip(t *testing.T) {
	left := make([]int64, 64)
	right := make([]int64, 64)
	for i := range left {
		left[i] = int64((i - 32) * 256)
		right[i] = left[i] + int64((i%3)-1)*2
	}
	data, err := encodeFLACFrameWithOptions([][]int64{left, right}, 12345, 16, 0, frameOptions{
		maxFixedOrder: 4, maxLPCOrder: 8, maxRicePartitionOrder: 6,
		enableWastedBits: true, enableStereoDecorrel: true,
		streamableSubset: false, variableBlocking: true,
	})
	if err != nil {
		t.Fatalf("encodeFLACFrameWithOptions() error = %v", err)
	}
	decoded, err := decodeFLACFrame(data, streamInfoFor(64, 12345, 2, 16))
	if err != nil {
		t.Fatalf("decodeFLACFrame() error = %v", err)
	}
	assertSamplesEqual(t, decoded.samples, [][]int64{left, right})
	if !decoded.header.blockingStrategy || decoded.header.sampleRate != 12345 {
		t.Fatalf("header = %+v, want variable blocking and uncommon sample rate", decoded.header)
	}
}

func TestEncodeFLACFrame_AllSupportedBitDepths(t *testing.T) {
	for _, bitsPerSample := range []int{4, 8, 12, 16, 20, 24, 32} {
		t.Run(fmt.Sprintf("%dbit", bitsPerSample), func(t *testing.T) {
			min := -(int64(1) << uint(bitsPerSample-1))
			max := (int64(1) << uint(bitsPerSample-1)) - 1
			data, err := encodeFLACFrameWithOptions([][]int64{{min, -1, 0, max}}, 44100, bitsPerSample, 0, frameOptions{
				maxFixedOrder: 4, maxLPCOrder: 4, maxRicePartitionOrder: 4,
				enableWastedBits: true, streamableSubset: false,
			})
			if err != nil {
				t.Fatalf("encodeFLACFrameWithOptions() error = %v", err)
			}
			decoded, err := decodeFLACFrame(data, streamInfoFor(4, 44100, 1, bitsPerSample))
			if err != nil {
				t.Fatalf("decodeFLACFrame() error = %v", err)
			}
			assertSamplesEqual(t, decoded.samples, [][]int64{{min, -1, 0, max}})
		})
	}
}

func TestWriteFrameHeader_UTF8BoundariesAndCRC(t *testing.T) {
	for _, frameNumber := range []uint64{0x7f, 0x80, 0x7ff, 0x800, 0xffff, 0x10000, 0x1fffff, 0x200000, 0x3ffffff, 0x4000000} {
		t.Run("frame", func(t *testing.T) {
			w := bits.NewWriter()
			if err := writeFrameHeader(w, 4096, 44100, 2, 16, frameNumber); err != nil {
				t.Fatalf("writeFrameHeader() error = %v", err)
			}
			data := w.Bytes()
			if hash.CRC8(data[:len(data)-1]) != data[len(data)-1] {
				t.Fatalf("invalid header CRC for frame number %#x", frameNumber)
			}
			r := bits.New(data)
			header, err := readFrameHeader(r, streamInfoFor(4096, 44100, 2, 16))
			if err != nil {
				t.Fatalf("readFrameHeader() error = %v", err)
			}
			if header.number != frameNumber {
				t.Fatalf("frame number = %#x, want %#x", header.number, frameNumber)
			}
		})
	}
}

func TestWriteResidualRoundtrip(t *testing.T) {
	residual := []int64{0, -1, 1, -2, 2, 3, -3, 7, -7}
	coding, ok := chooseRiceCoding(residual)
	if !ok {
		t.Fatal("chooseRiceCoding() failed")
	}
	w := bits.NewWriter()
	if err := writeResidual(w, residual, coding); err != nil {
		t.Fatalf("writeResidual() error = %v", err)
	}
	r := bits.New(w.Bytes())
	decoded, err := readResidual(r, len(residual), 0)
	if err != nil {
		t.Fatalf("readResidual() error = %v", err)
	}
	if !equalInt64s(decoded, residual) {
		t.Fatalf("residual = %v, want %v", decoded, residual)
	}
}

func TestWriteResidualRejectsSigned32Minimum(t *testing.T) {
	residual := []int64{-(int64(1) << 31), int64(1<<31) - 1}
	_, ok := chooseRiceCoding(residual)
	if ok {
		t.Fatal("chooseRiceCoding() accepted signed 32-bit minimum")
	}
}

func TestEncoderRejectsInvalidBlockSizeConfig(t *testing.T) {
	encoder := NewEncoder(EncoderConfig{BlockSize: 65536})
	frame := makeAudioFrameS16(t, media.LayoutMono1, 44100, 0, []int16{1})
	var wrapped media.Frame = frame
	if err := encoder.SendFrame(&wrapped); err == nil {
		t.Fatal("expected invalid block size configuration error")
	}
}

func TestEncoderRejectsNonSubsetBlockSizeAtLowSampleRate(t *testing.T) {
	encoder := NewEncoder(EncoderConfig{BlockSize: 4609, StreamableSubset: true})
	frame := makeAudioFrameS16(t, media.LayoutMono1, 44100, 0, []int16{1})
	var wrapped media.Frame = frame
	if err := encoder.SendFrame(&wrapped); err == nil {
		t.Fatal("expected streamable-subset block size error")
	}
}

func decodePacketSamples(t *testing.T, pkt *media.Packet, info streamInfo) [][]int64 {
	t.Helper()
	decoded, err := decodeFLACFrame(pkt.Data(), info)
	if err != nil {
		t.Fatalf("decodeFLACFrame() error = %v", err)
	}
	return decoded.samples
}

func makeAudioFrameS16(t *testing.T, layout media.ChannelLayout, sampleRate int, pts media.Pts, values []int16) *media.AudioFrame {
	t.Helper()
	channels := layout.ChannelCount()
	if len(values)%channels != 0 {
		t.Fatalf("values length %d is not divisible by channel count %d", len(values), channels)
	}
	frame := media.NewAudioFrame(media.SampleFormatS16, layout, sampleRate, len(values)/channels, media.WithAudioPts(pts))
	plane := frame.Planes()[0]
	for i, value := range values {
		binary.LittleEndian.PutUint16(plane[i*2:i*2+2], uint16(value))
	}
	return frame
}

func makeAudioFrameS32(t *testing.T, layout media.ChannelLayout, sampleRate int, pts media.Pts, bitsPerSample int, values []int32) *media.AudioFrame {
	t.Helper()
	channels := layout.ChannelCount()
	if len(values)%channels != 0 {
		t.Fatalf("values length %d is not divisible by channel count %d", len(values), channels)
	}
	frame := media.NewAudioFrame(media.SampleFormatS32, layout, sampleRate, len(values)/channels, media.WithAudioPts(pts), media.WithAudioBitsPerSample(bitsPerSample))
	plane := frame.Planes()[0]
	for i, value := range values {
		binary.LittleEndian.PutUint32(plane[i*4:i*4+4], uint32(value))
	}
	return frame
}

func streamInfoFor(blockSize, sampleRate, channels, bitsPerSample int) streamInfo {
	return streamInfo{
		MinBlockSize:  uint16(blockSize),
		MaxBlockSize:  uint16(blockSize),
		SampleRate:    sampleRate,
		Channels:      channels,
		BitsPerSample: bitsPerSample,
	}
}

func assertSamplesEqual(t *testing.T, got, want [][]int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("channel count = %d, want %d", len(got), len(want))
	}
	for ch := range want {
		if !equalInt64s(got[ch], want[ch]) {
			t.Fatalf("channel %d samples = %v, want %v", ch, got[ch], want[ch])
		}
	}
}

func equalInt64s(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBuildAudioFrameFromEncoded32Bit(t *testing.T) {
	data, err := encodeFLACFrame([][]int64{{-2_147_483_648, 0, 2_147_483_647}}, 44100, 32, 0, 4)
	if err != nil {
		t.Fatalf("encodeFLACFrame() error = %v", err)
	}
	decoded, err := decodeFLACFrame(data, streamInfoFor(3, 44100, 1, 32))
	if err != nil {
		t.Fatalf("decodeFLACFrame() error = %v", err)
	}
	frame, err := buildAudioFrame(decoded)
	if err != nil {
		t.Fatalf("buildAudioFrame() error = %v", err)
	}
	if frame.Format != media.SampleFormatS32 || frame.BitsPerSample != 32 {
		t.Fatalf("frame format/bps = %s/%d, want s32/32", frame.Format, frame.BitsPerSample)
	}
	wantBytes := make([]byte, 12)
	for i, value := range []int32{-2_147_483_648, 0, 2_147_483_647} {
		binary.LittleEndian.PutUint32(wantBytes[i*4:i*4+4], uint32(value))
	}
	if !bytes.Equal(frame.Planes()[0], wantBytes) {
		t.Fatalf("decoded frame bytes = % x, want % x", frame.Planes()[0], wantBytes)
	}
}
