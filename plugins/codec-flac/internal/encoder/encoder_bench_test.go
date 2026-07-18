package encoder

import (
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/godexture/codec-flac/internal/decoder"
	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/engine"
)

func benchmarkBlock(blockSize int) [][]int64 {
	left := make([]int64, blockSize)
	right := make([]int64, blockSize)
	state := uint32(0x12345678)
	for i := range left {
		state = state*1664525 + 1013904223
		noise := int64(int32(state)) >> 24
		tone := int64(12000 * math.Sin(2*math.Pi*float64(i)/128.3))
		left[i] = clamp16(tone + noise)
		state = state*1664525 + 1013904223
		right[i] = clamp16(tone + (int64(int32(state)) >> 24))
	}
	return [][]int64{left, right}
}

func clamp16(v int64) int64 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return v
}

func BenchmarkEncodeFrameDefaultConfig(b *testing.B) {
	block := benchmarkBlock(flac.DefaultEncoderConfig.BlockSize)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := EncodeFrame(block, 44100, 16, uint64(i), flac.DefaultEncoderConfig); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeFrameDefaultConfig(b *testing.B) {
	block := benchmarkBlock(flac.DefaultEncoderConfig.BlockSize)
	data, err := EncodeFrame(block, 44100, 16, 0, flac.DefaultEncoderConfig)
	if err != nil {
		b.Fatal(err)
	}
	info := streaminfo.StreamInfo{
		MinBlockSize:  uint16(flac.DefaultEncoderConfig.BlockSize),
		MaxBlockSize:  uint16(flac.DefaultEncoderConfig.BlockSize),
		SampleRate:    44100,
		Channels:      2,
		BitsPerSample: 16,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := decoder.DecodeFrame(data, info); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecoderValidationMode(b *testing.B) {
	block := benchmarkBlock(flac.DefaultEncoderConfig.BlockSize)
	data, err := EncodeFrame(block, 44100, 16, 0, flac.DefaultEncoderConfig)
	if err != nil {
		b.Fatal(err)
	}
	stream := media.StreamInfo{MediaAttributes: media.MediaAttributes{Audio: media.AudioAttributes{
		SampleRate: 44100, Format: media.SampleFormatS16, BitsPerSample: 16, ChannelLayout: media.LayoutStereo2_0,
	}}}
	packet := media.NewPacketFromData(data)

	decode := func(strict bool) {
		dec := decoder.NewDecoder(stream, flac.DecoderConfig{Strict: strict})
		if err := dec.SendPacket(packet); err != nil {
			b.Fatal(err)
		}
		if _, err := dec.ReceiveFrame(); err != nil {
			b.Fatal(err)
		}
	}

	var strictDuration, nonStrictDuration time.Duration
	var iterations int64
	b.ReportAllocs()
	for b.Loop() {
		if iterations&1 == 0 {
			start := time.Now()
			decode(true)
			strictDuration += time.Since(start)
			start = time.Now()
			decode(false)
			nonStrictDuration += time.Since(start)
		} else {
			start := time.Now()
			decode(false)
			nonStrictDuration += time.Since(start)
			start = time.Now()
			decode(true)
			strictDuration += time.Since(start)
		}
		iterations++
	}
	if iterations > 0 {
		b.ReportMetric(float64(strictDuration.Nanoseconds())/float64(iterations), "strict-ns/op")
		b.ReportMetric(float64(nonStrictDuration.Nanoseconds())/float64(iterations), "non-strict-ns/op")
	}
}

func BenchmarkEncoderDefaultConfig(b *testing.B) {
	cfg := flac.DefaultEncoderConfig
	blocks := 4
	samples := benchmarkBlock(cfg.BlockSize * blocks)
	frame := media.NewAudioFrame(media.SampleFormatS16, media.LayoutStereo2_0, 44100, cfg.BlockSize*blocks)
	plane := frame.Planes()[0]
	for sample := range samples[0] {
		for ch := range samples {
			offset := (sample*len(samples) + ch) * 2
			binary.LittleEndian.PutUint16(plane[offset:offset+2], uint16(int16(samples[ch][sample])))
		}
	}
	var wrapped media.Frame = frame
	b.ReportAllocs()
	b.SetBytes(int64(len(plane)))
	b.ResetTimer()
	for b.Loop() {
		enc, err := NewEncoder(media.StreamInfo{}, cfg)
		if err != nil {
			b.Fatal(err)
		}
		if err := enc.SendFrame(&wrapped); err != nil {
			b.Fatal(err)
		}
		for packets := 0; ; packets++ {
			packet, err := enc.ReceivePacket()
			if err == engine.ErrEAGAIN {
				if packets != blocks {
					b.Fatalf("received %d packets, want %d", packets, blocks)
				}
				break
			}
			if err != nil {
				b.Fatal(err)
			}
			packet.Release()
		}
		if err := enc.Flush(); err != nil {
			b.Fatal(err)
		}
		for {
			packet, err := enc.ReceivePacket()
			if err == engine.ErrEOF {
				break
			}
			if err != nil {
				b.Fatal(err)
			}
			packet.Release()
		}
	}
}
