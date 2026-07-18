package encoder

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/godexture/codec-flac/internal/decoder"
	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/frame"
	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/hash"
)

func TestEncoder_ReceivePacketEmptyActive(t *testing.T) {
	t.Parallel()
	enc, err := NewEncoder(media.StreamInfo{}, flac.DefaultEncoderConfig)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}
	pkt, err := enc.ReceivePacket()
	if !errors.Is(err, engine.ErrEAGAIN) || pkt != nil {
		t.Fatalf("expected ErrEAGAIN and nil packet, got err=%v, packet=%v", err, pkt)
	}
}

func TestEncoder_ReceivePacketEmptyFlushed(t *testing.T) {
	t.Parallel()
	enc, err := NewEncoder(media.StreamInfo{}, flac.DefaultEncoderConfig)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}

	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	assertMD5EndPacket(t, receivePacket(t, enc), md5.Sum(nil))
	pkt, err := enc.ReceivePacket()
	if !errors.Is(err, engine.ErrEOF) || pkt != nil {
		t.Fatalf("expected ErrEOF after end packet, got err=%v, packet=%v", err, pkt)
	}
}

func TestEncoder_SendFrameAfterFlush(t *testing.T) {
	t.Parallel()
	enc, err := NewEncoder(media.StreamInfo{}, flac.DefaultEncoderConfig)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}

	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	frame := makeAudioFrameS16(t, media.LayoutMono1, 44100, 0, []int16{1})
	var wrapped media.Frame = frame
	if err := enc.SendFrame(&wrapped); !errors.Is(err, engine.ErrEOF) {
		t.Fatalf("expected ErrEOF after flush, got %v", err)
	}
}

func TestEncoder_SendNilFrame(t *testing.T) {
	t.Parallel()
	enc, err := NewEncoder(media.StreamInfo{}, flac.DefaultEncoderConfig)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}

	if err := enc.SendFrame(nil); err == nil {
		t.Fatal("expected error for nil frame")
	}
}

func TestEncoder_S16StereoRoundtrip(t *testing.T) {
	t.Parallel()
	cfg := flac.DefaultEncoderConfig
	cfg.BlockSize = 16
	enc, err := NewEncoder(media.StreamInfo{}, cfg)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}

	input := []int16{0, 100, 1, 99, 2, 98, 3, 97}
	frame := makeAudioFrameS16(t, media.LayoutStereo2_0, 44100, 42, input)
	var wrapped media.Frame = frame
	if err := enc.SendFrame(&wrapped); err != nil {
		t.Fatalf("SendFrame() error = %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	pkt, err := enc.ReceivePacket()
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
	t.Parallel()
	cfg := flac.DefaultEncoderConfig
	cfg.BlockSize = 16
	enc, err := NewEncoder(media.StreamInfo{}, cfg)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}

	frame := makeAudioFrameS16(t, media.LayoutMono1, 44100, 7, []int16{1, 2, 3})
	var wrapped media.Frame = frame
	if err := enc.SendFrame(&wrapped); err != nil {
		t.Fatalf("SendFrame() error = %v", err)
	}
	if pkt, err := enc.ReceivePacket(); !errors.Is(err, engine.ErrEAGAIN) || pkt != nil {
		t.Fatalf("ReceivePacket() before flush = (%v, %v), want (nil, ErrEAGAIN)", pkt, err)
	}

	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	pkt, err := enc.ReceivePacket()
	if err != nil {
		t.Fatalf("ReceivePacket() after flush error = %v", err)
	}
	decoded := decodePacketSamples(t, pkt, streamInfoFor(3, 44100, 1, 16))
	assertSamplesEqual(t, decoded, [][]int64{{1, 2, 3}})
	assertMD5EndPacket(t, receivePacket(t, enc), md5.Sum(frame.Planes()[0]))
	if pkt, err := enc.ReceivePacket(); !errors.Is(err, engine.ErrEOF) || pkt != nil {
		t.Fatalf("ReceivePacket() after end packet = (%v, %v), want (nil, ErrEOF)", pkt, err)
	}
}

func TestChooseBlockSplit_SelectsSignalBoundaries(t *testing.T) {
	t.Parallel()
	block := make([]int64, 4096)
	for i := range block {
		frequency := 0.02
		if i >= len(block)/2 {
			frequency = 0.31
		}
		block[i] = int64(math.Round(12000 * math.Sin(float64(i)*frequency)))
	}
	for _, mode := range []flac.BlockSplitMode{flac.BlockSplitEstimated, flac.BlockSplitExact} {
		t.Run(fmt.Sprintf("mode=%d", mode), func(t *testing.T) {
			cfg := flac.DefaultEncoderConfig
			cfg.BlockSplitDepth, cfg.BlockSplitMode = 2, mode
			windows := newWindowSet(cfg.Apodizations)
			spans, err := chooseBlockSplit([][]int64{block}, 16, cfg, &windows)
			if err != nil {
				t.Fatal(err)
			}
			defer releaseBlockSpans(spans)
			if len(spans) < 2 {
				t.Fatalf("selected %d span, want adaptive split", len(spans))
			}
			offset := 0
			for _, span := range spans {
				if span.offset != offset || span.length < 1024 {
					t.Fatalf("invalid span: %+v", span)
				}
				offset += span.length
			}
			if offset != len(block) {
				t.Fatalf("span total = %d, want %d", offset, len(block))
			}
		})
	}
}

func TestChooseBlockSplit_KeepsUniformSignalWhole(t *testing.T) {
	t.Parallel()
	for _, mode := range []flac.BlockSplitMode{flac.BlockSplitEstimated, flac.BlockSplitExact} {
		t.Run(fmt.Sprintf("mode=%d", mode), func(t *testing.T) {
			cfg := flac.DefaultEncoderConfig
			cfg.BlockSplitDepth, cfg.BlockSplitMode = 2, mode
			windows := newWindowSet(cfg.Apodizations)
			spans, err := chooseBlockSplit([][]int64{make([]int64, 4096)}, 16, cfg, &windows)
			if err != nil {
				t.Fatal(err)
			}
			defer releaseBlockSpans(spans)
			if len(spans) != 1 || spans[0].length != 4096 {
				t.Fatalf("spans = %+v, want one full block", spans)
			}
		})
	}
}

func TestEncoder_AdaptiveBlocksPreserveNumbersPTSAndSamples(t *testing.T) {
	t.Parallel()
	input := make([]int16, 4096)
	for i := range input {
		frequency := 0.02
		if i >= len(input)/2 {
			frequency = 0.31
		}
		input[i] = int16(math.Round(12000 * math.Sin(float64(i)*frequency)))
	}
	for _, mode := range []flac.BlockSplitMode{flac.BlockSplitEstimated, flac.BlockSplitExact} {
		t.Run(fmt.Sprintf("mode=%d", mode), func(t *testing.T) {
			cfg := flac.DefaultEncoderConfig
			cfg.BlockSplitDepth, cfg.BlockSplitMode = 2, mode
			enc, err := NewEncoder(media.StreamInfo{}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			inputFrame := makeAudioFrameS16(t, media.LayoutMono1, 44100, 17, input)
			var wrapped media.Frame = inputFrame
			if err := enc.SendFrame(&wrapped); err != nil {
				t.Fatal(err)
			}
			if err := enc.Flush(); err != nil {
				t.Fatal(err)
			}

			var actual []int64
			var number uint64
			for frames := 0; ; frames++ {
				packet := receivePacket(t, enc)
				if packet.Kind == media.PacketKindStreamEnd {
					packet.Release()
					if frames < 2 {
						t.Fatalf("encoded %d frame, want adaptive splitting", frames)
					}
					break
				}
				decoded, err := decoder.DecodeFrame(packet.Data(), streamInfoFor(4096, 44100, 1, 16))
				if err != nil {
					t.Fatal(err)
				}
				if !decoded.Header.BlockingStrategy || decoded.Header.Number != number || packet.PTS != media.Pts(17+number) {
					t.Fatalf("header/PTS = %+v/%d, want sample %d", decoded.Header, packet.PTS, number)
				}
				number += uint64(decoded.Header.BlockSize)
				actual = append(actual, decoded.Samples[0]...)
				packet.Release()
			}
			if number != uint64(len(input)) {
				t.Fatalf("sample total = %d, want %d", number, len(input))
			}
			want := make([]int64, len(input))
			for i := range input {
				want[i] = int64(input[i])
			}
			assertSamplesEqual(t, [][]int64{actual}, [][]int64{want})
		})
	}
}

func TestEncoder_ArbitraryInputChunksPreserveSamplesAndPTS(t *testing.T) {
	t.Parallel()
	cfg := flac.DefaultEncoderConfig
	cfg.BlockSize = 16
	enc, err := NewEncoder(media.StreamInfo{}, cfg)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}
	chunks := []struct {
		pts    media.Pts
		values []int16
	}{
		{0, []int16{0}},
		{1, []int16{1, 2, 3, 4, 5}},
		{6, []int16{6, 7}},
	}
	for _, chunk := range chunks {
		frame := makeAudioFrameS16(t, media.LayoutMono1, 44100, chunk.pts, chunk.values)
		var wrapped media.Frame = frame
		if err := enc.SendFrame(&wrapped); err != nil {
			t.Fatalf("SendFrame(%d) error = %v", chunk.pts, err)
		}
	}

	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	for packetIndex, want := range []struct {
		pts     media.Pts
		samples []int64
	}{{0, []int64{0, 1, 2, 3, 4, 5, 6, 7}}} {
		packet, err := enc.ReceivePacket()
		if err != nil {
			t.Fatalf("ReceivePacket(%d) error = %v", packetIndex, err)
		}
		if packet.PTS != want.pts {
			t.Errorf("packet %d PTS = %d, want %d", packetIndex, packet.PTS, want.pts)
		}
		decoded := decodePacketSamples(t, packet, streamInfoFor(4, 44100, 1, 16))
		assertSamplesEqual(t, decoded, [][]int64{want.samples})
		packet.Release()
	}
	end := receivePacket(t, enc)
	if end.Kind != media.PacketKindStreamEnd {
		t.Fatalf("final packet kind = %d, want stream end", end.Kind)
	}
	end.Release()
	if enc.pendingQueue != nil {
		t.Fatalf("pending queue retained after drain: len=%d cap=%d", len(enc.pendingQueue), cap(enc.pendingQueue))
	}
}

func receivePacket(t *testing.T, enc *Encoder) *media.Packet {
	t.Helper()
	pkt, err := enc.ReceivePacket()
	if err != nil {
		t.Fatalf("ReceivePacket() error = %v", err)
	}
	return pkt
}

func assertMD5EndPacket(t *testing.T, pkt *media.Packet, want [16]byte) {
	t.Helper()
	defer pkt.Release()
	if pkt.Kind != media.PacketKindStreamEnd {
		t.Fatalf("packet kind = %d, want stream end", pkt.Kind)
	}
	if len(pkt.CodecParameters) != 1 {
		t.Fatalf("packet codec parameters = %#v, want one", pkt.CodecParameters)
	}
	param := pkt.CodecParameters[0]
	if !media.IsCodecParameters[streaminfo.PCMMD5Parameters](param) || !bytes.Equal(param.Data, want[:]) {
		t.Fatalf("packet MD5 parameter = %#v, want %x", param, want)
	}
}

func TestDecoderWorkspaceDoesNotMutateReturnedFrames(t *testing.T) {
	t.Parallel()
	cfg := flac.DefaultEncoderConfig
	cfg.BlockSize = 16
	// This test asserts decoder-side buffer-reuse safety and relies on
	// ReceivePacket returning already-encoded packets synchronously without
	// calling Flush first; that only holds for the sequential (Workers<=1)
	// path, so pin it explicitly rather than retry-looping on ErrEAGAIN.
	cfg.Workers = 1
	enc, err := NewEncoder(media.StreamInfo{}, cfg)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}
	input := make([]int16, 32)
	for i := range input {
		input[i] = int16(i)
	}
	frame := makeAudioFrameS16(t, media.LayoutMono1, 44100, 0, input)
	var wrapped media.Frame = frame
	if err := enc.SendFrame(&wrapped); err != nil {
		t.Fatalf("SendFrame() error = %v", err)
	}
	firstPacket, err := enc.ReceivePacket()
	if err != nil {
		t.Fatalf("ReceivePacket(first) error = %v", err)
	}
	secondPacket, err := enc.ReceivePacket()
	if err != nil {
		t.Fatalf("ReceivePacket(second) error = %v", err)
	}
	dec := decoder.NewDecoder(media.StreamInfo{MediaAttributes: media.MediaAttributes{Audio: media.AudioAttributes{
		SampleRate: 44100, Format: media.SampleFormatS16, BitsPerSample: 16, ChannelLayout: media.LayoutMono1,
	}}}, flac.DecoderConfig{})
	if err := dec.SendPacket(firstPacket); err != nil {
		t.Fatalf("decoder SendPacket(first) error = %v", err)
	}
	firstPacket.Release()
	first, err := dec.ReceiveFrame()
	if err != nil {
		t.Fatalf("ReceiveFrame(first) error = %v", err)
	}
	firstAudio := (*first).(*media.AudioFrame)
	firstPCM := append([]byte(nil), firstAudio.Planes()[0]...)
	if err := dec.SendPacket(secondPacket); err != nil {
		t.Fatalf("decoder SendPacket(second) error = %v", err)
	}
	secondPacket.Release()
	second, err := dec.ReceiveFrame()
	if err != nil {
		t.Fatalf("ReceiveFrame(second) error = %v", err)
	}
	if !bytes.Equal(firstAudio.Planes()[0], firstPCM) {
		t.Fatal("first returned frame was mutated while decoding the second frame")
	}
	secondAudio := (*second).(*media.AudioFrame)
	if bytes.Equal(firstAudio.Planes()[0], secondAudio.Planes()[0]) {
		t.Fatal("test frames unexpectedly contain identical PCM")
	}
}

func TestEncoder_S32As24BitRoundtrip(t *testing.T) {
	t.Parallel()
	cfg := flac.DefaultEncoderConfig
	cfg.BlockSize = 16
	cfg.BitsPerSample = 24
	enc, err := NewEncoder(media.StreamInfo{}, cfg)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}

	input := []int32{-8_388_608, -1, 0, 8_388_607}
	frame := makeAudioFrameS32(t, media.LayoutMono1, 48000, 0, 24, input)
	var wrapped media.Frame = frame
	if err := enc.SendFrame(&wrapped); err != nil {
		t.Fatalf("SendFrame() error = %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	pkt, err := enc.ReceivePacket()
	if err != nil {
		t.Fatalf("ReceivePacket() error = %v", err)
	}
	decoded := decodePacketSamples(t, pkt, streamInfoFor(4, 48000, 1, 24))
	assertSamplesEqual(t, decoded, [][]int64{{-8_388_608, -1, 0, 8_388_607}})
}

func TestEncoder_S32As32BitRoundtrip(t *testing.T) {
	t.Parallel()
	cfg := flac.DefaultEncoderConfig
	cfg.BlockSize = 16
	cfg.BitsPerSample = 32
	enc, err := NewEncoder(media.StreamInfo{}, cfg)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}
	input := []int32{-2_147_483_648, -1, 0, 2_147_483_647}
	frame := makeAudioFrameS32(t, media.LayoutMono1, 96000, 0, 32, input)
	var wrapped media.Frame = frame
	if err := enc.SendFrame(&wrapped); err != nil {
		t.Fatalf("SendFrame() error = %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	pkt, err := enc.ReceivePacket()
	if err != nil {
		t.Fatalf("ReceivePacket() error = %v", err)
	}
	decoded := decodePacketSamples(t, pkt, streamInfoFor(4, 96000, 1, 32))
	assertSamplesEqual(t, decoded, [][]int64{{-2_147_483_648, -1, 0, 2_147_483_647}})
}

func TestEncoder_S24As24BitRoundtrip(t *testing.T) {
	t.Parallel()
	cfg := flac.DefaultEncoderConfig
	cfg.BlockSize = 16
	cfg.BitsPerSample = 24
	enc, err := NewEncoder(media.StreamInfo{}, cfg)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}

	input := []int32{-8_388_608, -1, 0, 8_388_607}
	frame := makeAudioFrameS24(t, media.LayoutMono1, 48000, 0, 24, input)
	var wrapped media.Frame = frame
	if err := enc.SendFrame(&wrapped); err != nil {
		t.Fatalf("SendFrame() error = %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	pkt, err := enc.ReceivePacket()
	if err != nil {
		t.Fatalf("ReceivePacket() error = %v", err)
	}
	decoded := decodePacketSamples(t, pkt, streamInfoFor(4, 48000, 1, 24))
	assertSamplesEqual(t, decoded, [][]int64{{-8_388_608, -1, 0, 8_388_607}})
}

func TestEncoder_S24As20BitRoundtrip(t *testing.T) {
	t.Parallel()
	cfg := flac.DefaultEncoderConfig
	cfg.BlockSize = 16
	cfg.BitsPerSample = 20
	enc, err := NewEncoder(media.StreamInfo{}, cfg)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}

	input := []int32{-524_288, -1, 0, 524_287}
	frame := makeAudioFrameS24(t, media.LayoutMono1, 48000, 0, 20, input)
	var wrapped media.Frame = frame
	if err := enc.SendFrame(&wrapped); err != nil {
		t.Fatalf("SendFrame() error = %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	pkt, err := enc.ReceivePacket()
	if err != nil {
		t.Fatalf("ReceivePacket() error = %v", err)
	}
	decoded := decodePacketSamples(t, pkt, streamInfoFor(4, 48000, 1, 20))
	assertSamplesEqual(t, decoded, [][]int64{{-524_288, -1, 0, 524_287}})
}

func TestEncoder_Rejects24BitOutOfRange(t *testing.T) {
	t.Parallel()
	cfg := flac.DefaultEncoderConfig
	cfg.BlockSize = 16
	cfg.BitsPerSample = 24
	enc, err := NewEncoder(media.StreamInfo{}, cfg)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}

	frame := makeAudioFrameS32(t, media.LayoutMono1, 44100, 0, 24, []int32{8_388_608})
	var wrapped media.Frame = frame
	if err := enc.SendFrame(&wrapped); err == nil {
		t.Fatal("expected out-of-range S32/24-bit error")
	}
}

func TestEncoder_RejectsStreamChange(t *testing.T) {
	t.Parallel()
	cfg := flac.DefaultEncoderConfig
	cfg.BlockSize = 16
	enc, err := NewEncoder(media.StreamInfo{}, cfg)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}

	first := makeAudioFrameS16(t, media.LayoutMono1, 44100, 0, []int16{1})
	var firstWrapped media.Frame = first
	if err := enc.SendFrame(&firstWrapped); err != nil {
		t.Fatalf("SendFrame(first) error = %v", err)
	}

	second := makeAudioFrameS16(t, media.LayoutMono1, 48000, 1, []int16{2})
	var secondWrapped media.Frame = second
	if err := enc.SendFrame(&secondWrapped); err == nil {
		t.Fatal("expected stream change error")
	}
}

func TestEncodeFrame_ConstantSubframeRoundtrip(t *testing.T) {
	t.Parallel()
	cfg := flac.DefaultEncoderConfig
	data, err := EncodeFrame([][]int64{{123, 123, 123, 123}}, 44100, 16, 0, cfg)
	if err != nil {
		t.Fatalf("EncodeFrame() error = %v", err)
	}
	decoded, err := decoder.DecodeFrame(data, streamInfoFor(4, 44100, 1, 16))
	if err != nil {
		t.Fatalf("DecodeFrame() error = %v", err)
	}
	assertSamplesEqual(t, decoded.Samples, [][]int64{{123, 123, 123, 123}})
}

func TestEncodeFrame_FixedSubframeRoundtrip(t *testing.T) {
	t.Parallel()
	cfg := flac.DefaultEncoderConfig
	data, err := EncodeFrame([][]int64{{0, 1, 2, 3, 4, 5, 6, 7}}, 44100, 16, 12, cfg)
	if err != nil {
		t.Fatalf("EncodeFrame() error = %v", err)
	}
	decoded, err := decoder.DecodeFrame(data, streamInfoFor(8, 44100, 1, 16))
	if err != nil {
		t.Fatalf("DecodeFrame() error = %v", err)
	}
	if decoded.Header.Number != 12 {
		t.Fatalf("frame number = %d, want 12", decoded.Header.Number)
	}
	assertSamplesEqual(t, decoded.Samples, [][]int64{{0, 1, 2, 3, 4, 5, 6, 7}})
}

func TestEncodeFrame_VerbatimFallbackRoundtrip(t *testing.T) {
	t.Parallel()
	cfg := flac.DefaultEncoderConfig
	data, err := EncodeFrame([][]int64{{-2_147_483_648, 0, 2_147_483_647}}, 44100, 32, 0, cfg)
	if err != nil {
		t.Fatalf("EncodeFrame() error = %v", err)
	}
	decoded, err := decoder.DecodeFrame(data, streamInfoFor(3, 44100, 1, 32))
	if err != nil {
		t.Fatalf("DecodeFrame() error = %v", err)
	}
	assertSamplesEqual(t, decoded.Samples, [][]int64{{-2_147_483_648, 0, 2_147_483_647}})
}

func TestEncodeFrame_FullBitstreamFeaturesRoundtrip(t *testing.T) {
	t.Parallel()
	left := make([]int64, 64)
	right := make([]int64, 64)
	for i := range left {
		left[i] = int64((i - 32) * 256)
		right[i] = left[i] + int64((i%3)-1)*2
	}
	cfg := flac.EncoderConfig{
		MaxFixedOrder: 4, MaxLPCOrder: 8, MaxRicePartitionOrder: 6,
		EnableWastedBits: true, StereoMode: flac.StereoExhaustive,
		StreamableSubset: false, BlockSplitDepth: 1,
	}
	data, err := EncodeFrame([][]int64{left, right}, 12345, 16, 0, cfg)
	if err != nil {
		t.Fatalf("EncodeFrame() error = %v", err)
	}
	decoded, err := decoder.DecodeFrame(data, streamInfoFor(64, 12345, 2, 16))
	if err != nil {
		t.Fatalf("DecodeFrame() error = %v", err)
	}
	assertSamplesEqual(t, decoded.Samples, [][]int64{left, right})
	if !decoded.Header.BlockingStrategy || decoded.Header.SampleRate != 12345 {
		t.Fatalf("header = %+v, want variable blocking and uncommon sample rate", decoded.Header)
	}
}

func TestEncodeFrame_SearchModesRoundtrip(t *testing.T) {
	t.Parallel()
	samples := make([]int64, 256)
	for i := range samples {
		samples[i] = int64((i*i*17)%65536) - 32768
	}
	for _, exhaustive := range []bool{false, true} {
		t.Run(fmt.Sprintf("exhaustive=%t", exhaustive), func(t *testing.T) {
			t.Parallel()
			cfg := flac.DefaultEncoderConfig
			cfg.StreamableSubset = false
			if exhaustive {
				cfg.FixedOrderSearch, cfg.LPCOrderSearch, cfg.RiceCost = flac.OrderSearchExhaustive, flac.OrderSearchExhaustive, flac.RiceCostExact
			}
			data, err := EncodeFrame([][]int64{samples}, 44100, 16, 0, cfg)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decoder.DecodeFrame(data, streamInfoFor(len(samples), 44100, 1, 16))
			if err != nil {
				t.Fatal(err)
			}
			assertSamplesEqual(t, decoded.Samples, [][]int64{samples})
		})
	}
}

func TestEncodeFrame_AllSupportedBitDepths(t *testing.T) {
	t.Parallel()
	for _, bitsPerSample := range []int{4, 8, 12, 16, 20, 24, 32} {
		t.Run(fmt.Sprintf("%dbit", bitsPerSample), func(t *testing.T) {
			t.Parallel()
			min := -(int64(1) << uint(bitsPerSample-1))
			max := (int64(1) << uint(bitsPerSample-1)) - 1
			cfg := flac.EncoderConfig{
				MaxFixedOrder: 4, MaxLPCOrder: 4, MaxRicePartitionOrder: 4,
				EnableWastedBits: true, StreamableSubset: false,
			}
			data, err := EncodeFrame([][]int64{{min, -1, 0, max}}, 44100, bitsPerSample, 0, cfg)
			if err != nil {
				t.Fatalf("EncodeFrame() error = %v", err)
			}
			decoded, err := decoder.DecodeFrame(data, streamInfoFor(4, 44100, 1, bitsPerSample))
			if err != nil {
				t.Fatalf("DecodeFrame() error = %v", err)
			}
			assertSamplesEqual(t, decoded.Samples, [][]int64{{min, -1, 0, max}})
		})
	}
}

func TestWriteFrameHeader_UTF8BoundariesAndCRC(t *testing.T) {
	t.Parallel()
	for _, frameNumber := range []uint64{0x7f, 0x80, 0x7ff, 0x800, 0xffff, 0x10000, 0x1fffff, 0x200000, 0x3ffffff, 0x4000000} {
		t.Run(fmt.Sprintf("frame-%x", frameNumber), func(t *testing.T) {
			t.Parallel()
			w := bits.NewWriter()
			header := &frame.Header{
				BlockSize: 4096, SampleRate: 44100, Channels: 2, ChannelAssignment: 1, BitsPerSample: 16, Number: frameNumber,
			}
			if err := frame.EncodeHeader(w, header, false); err != nil {
				t.Fatalf("EncodeHeader() error = %v", err)
			}
			data := w.Bytes()
			if hash.CRC8(data[:len(data)-1]) != data[len(data)-1] {
				t.Fatalf("invalid header CRC for frame number %#x", frameNumber)
			}
			decHeader, err := frame.ParseHeader(data, streamInfoFor(4096, 44100, 2, 16))
			if err != nil {
				t.Fatalf("ParseHeader() error = %v", err)
			}
			if decHeader.Number != frameNumber {
				t.Fatalf("frame number = %#x, want %#x", decHeader.Number, frameNumber)
			}
		})
	}
}

func TestWriteResidualRoundtrip(t *testing.T) {
	t.Parallel()
	residual := []int64{0, -1, 1, -2, 2, 3, -3, 7, -7}
	coding, ok := chooseRiceCoding(residual)
	if !ok {
		t.Fatal("chooseRiceCoding() failed")
	}
	w := bits.NewWriter()
	if err := EncodeResidual(w, residual, coding); err != nil {
		t.Fatalf("EncodeResidual() error = %v", err)
	}
	r := bits.New(w.Bytes())
	decoded, err := decoder.DecodeResidual(r, len(residual), 0)
	if err != nil {
		t.Fatalf("DecodeResidual() error = %v", err)
	}
	if !equalInt64s(decoded, residual) {
		t.Fatalf("residual = %v, want %v", decoded, residual)
	}
}

func TestWriteResidualRejectsSigned32Minimum(t *testing.T) {
	t.Parallel()
	residual := []int64{-(int64(1) << 31), int64(1<<31) - 1}
	_, ok := chooseRiceCoding(residual)
	if ok {
		t.Fatal("chooseRiceCoding() accepted signed 32-bit minimum")
	}
}

func TestEncoderRejectsInvalidBlockSizeConfig(t *testing.T) {
	t.Parallel()
	for _, blockSize := range []int{15, 65536} {
		cfg := flac.EncoderConfig{BlockSize: blockSize}
		if _, err := NewEncoder(media.StreamInfo{}, cfg); err == nil {
			t.Fatalf("expected block size %d to be rejected", blockSize)
		}
	}
}

func TestEncoderRejectsNonSubsetBlockSizeAtLowSampleRate(t *testing.T) {
	t.Parallel()
	cfg := flac.EncoderConfig{BlockSize: 4609, StreamableSubset: true}
	enc, err := NewEncoder(media.StreamInfo{}, cfg)
	if err != nil {
		t.Fatalf("NewEncoder(media.StreamInfo{}, ) error = %v", err)
	}

	frame := makeAudioFrameS16(t, media.LayoutMono1, 44100, 0, []int16{1})
	var wrapped media.Frame = frame
	if err := enc.SendFrame(&wrapped); err == nil {
		t.Fatal("expected streamable-subset block size error")
	}
}

// TestEncoder_WorkersDoesNotChangeOutput is the determinism guarantee for
// the parallel encode path (see internal/flac/config.go's Workers doc
// comment): the same config and input must produce byte-identical packets
// regardless of how many workers process the blocks concurrently.
func TestEncoder_WorkersDoesNotChangeOutput(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(42))
	input := make([]int16, 4096*6+777) // several full blocks plus a partial tail
	for i := range input {
		input[i] = int16(rng.Intn(65536) - 32768)
	}

	for _, mode := range []flac.BlockSplitMode{flac.BlockSplitEstimated, flac.BlockSplitExact} {
		t.Run(fmt.Sprintf("mode=%d", mode), func(t *testing.T) {
			t.Parallel()
			base := flac.DefaultEncoderConfig
			base.BlockSize = 1024
			base.BlockSplitDepth, base.BlockSplitMode = 2, mode

			sequential := base
			sequential.Workers = 1
			parallel := base
			parallel.Workers = 8

			wantPackets := encodeAllPackets(t, sequential, input)
			gotPackets := encodeAllPackets(t, parallel, input)

			if len(gotPackets) != len(wantPackets) {
				t.Fatalf("packet count = %d, want %d", len(gotPackets), len(wantPackets))
			}
			for i := range wantPackets {
				if !bytes.Equal(gotPackets[i], wantPackets[i]) {
					t.Fatalf("packet %d differs between Workers=1 and Workers=8", i)
				}
			}
		})
	}
}

// TestEncoder_CloseReleasesWorkersWithoutFlush covers the goroutine-leak fix:
// Close() must terminate the worker pool (by closing e.jobs) even when
// Flush() is never called, which is exactly what happens when a pipeline
// aborts via error or context cancellation before reaching end-of-stream.
func TestEncoder_CloseReleasesWorkersWithoutFlush(t *testing.T) {
	t.Parallel()
	cfg := flac.DefaultEncoderConfig
	cfg.Workers = 4
	enc, err := NewEncoder(media.StreamInfo{}, cfg)
	if err != nil {
		t.Fatalf("NewEncoder() error = %v", err)
	}

	if err := enc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case _, ok := <-enc.jobs:
		if ok {
			t.Fatal("jobs channel received a value instead of reporting closed")
		}
	default:
		t.Fatal("jobs channel is not closed")
	}
}

// TestEncoder_CloseAndFlushIdempotent covers both call orders between Close
// and Flush: whichever runs first must not cause the other to double-close
// e.jobs (which would panic).
func TestEncoder_CloseAndFlushIdempotent(t *testing.T) {
	t.Parallel()

	t.Run("FlushThenClose", func(t *testing.T) {
		t.Parallel()
		cfg := flac.DefaultEncoderConfig
		cfg.Workers = 4
		enc, err := NewEncoder(media.StreamInfo{}, cfg)
		if err != nil {
			t.Fatalf("NewEncoder() error = %v", err)
		}
		if err := enc.Flush(); err != nil {
			t.Fatalf("Flush() error = %v", err)
		}
		if err := enc.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	t.Run("CloseThenFlush", func(t *testing.T) {
		t.Parallel()
		cfg := flac.DefaultEncoderConfig
		cfg.Workers = 4
		enc, err := NewEncoder(media.StreamInfo{}, cfg)
		if err != nil {
			t.Fatalf("NewEncoder() error = %v", err)
		}
		if err := enc.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if err := enc.Flush(); err != nil {
			t.Fatalf("Flush() error = %v", err)
		}
	})

	t.Run("CloseTwice", func(t *testing.T) {
		t.Parallel()
		cfg := flac.DefaultEncoderConfig
		cfg.Workers = 4
		enc, err := NewEncoder(media.StreamInfo{}, cfg)
		if err != nil {
			t.Fatalf("NewEncoder() error = %v", err)
		}
		if err := enc.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if err := enc.Close(); err != nil {
			t.Fatalf("second Close() error = %v", err)
		}
	})
}

// encodeAllPackets runs cfg over input to completion and returns each
// packet's raw bytes in emission order (the StreamEnd event packet, which
// carries no frame Data, contributes an empty slice so packet counts and
// positions stay directly comparable across runs).
func encodeAllPackets(t *testing.T, cfg flac.EncoderConfig, input []int16) [][]byte {
	t.Helper()
	enc, err := NewEncoder(media.StreamInfo{}, cfg)
	if err != nil {
		t.Fatalf("NewEncoder() error = %v", err)
	}
	frame := makeAudioFrameS16(t, media.LayoutMono1, 44100, 0, input)
	var wrapped media.Frame = frame
	if err := enc.SendFrame(&wrapped); err != nil {
		t.Fatalf("SendFrame() error = %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	var packets [][]byte
	for {
		pkt, err := enc.ReceivePacket()
		if errors.Is(err, engine.ErrEOF) {
			return packets
		}
		if err != nil {
			t.Fatalf("ReceivePacket() error = %v", err)
		}
		packets = append(packets, append([]byte(nil), pkt.Data()...))
		pkt.Release()
	}
}

func decodePacketSamples(t *testing.T, pkt *media.Packet, info streaminfo.StreamInfo) [][]int64 {
	t.Helper()
	decoded, err := decoder.DecodeFrame(pkt.Data(), info)
	if err != nil {
		t.Fatalf("DecodeFrame() error = %v", err)
	}
	return decoded.Samples
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

func makeAudioFrameS24(t *testing.T, layout media.ChannelLayout, sampleRate int, pts media.Pts, bitsPerSample int, values []int32) *media.AudioFrame {
	t.Helper()
	channels := layout.ChannelCount()
	if len(values)%channels != 0 {
		t.Fatalf("values length %d is not divisible by channel count %d", len(values), channels)
	}
	frame := media.NewAudioFrame(media.SampleFormatS24, layout, sampleRate, len(values)/channels, media.WithAudioPts(pts), media.WithAudioBitsPerSample(bitsPerSample))
	plane := frame.Planes()[0]
	for i, value := range values {
		plane[i*3] = byte(value)
		plane[i*3+1] = byte(value >> 8)
		plane[i*3+2] = byte(value >> 16)
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

func streamInfoFor(blockSize, sampleRate, channels, bitsPerSample int) streaminfo.StreamInfo {
	return streaminfo.StreamInfo{
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
