package audio

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil/audio/pcm"
)

const syntheticPacketSize = 4096

type syntheticDemuxer struct {
	remaining int
	stream    media.StreamInfo
}

func (d *syntheticDemuxer) Analyze() ([]media.StreamInfo, metadata.Bundle, error) {
	return []media.StreamInfo{d.stream}, metadata.Bundle{}, nil
}

func (d *syntheticDemuxer) ReadPacket() (*media.Packet, int, error) {
	if d.remaining == 0 {
		return nil, 0, io.EOF
	}
	size := min(syntheticPacketSize, d.remaining)
	packet := media.NewPacket(size)
	for i := range packet.Data() {
		packet.Data()[i] = byte(i)
	}
	d.remaining -= size
	return packet, 0, nil
}

type syntheticDecoder struct {
	pending *media.Packet
	flushed bool
	attrs   media.AudioAttributes
}

func (d *syntheticDecoder) SendPacket(packet *media.Packet) error {
	d.pending = packet
	return nil
}

func (d *syntheticDecoder) ReceiveFrame() (*media.Frame, error) {
	if d.pending == nil {
		if d.flushed {
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}
	frame := media.NewAudioFrame(d.attrs.Format, d.attrs.ChannelLayout, d.attrs.SampleRate, len(d.pending.Data())/2,
		media.WithAudioBitsPerSample(d.attrs.BitsPerSample))
	copy(frame.Planes()[0], d.pending.Data())
	d.pending = nil
	var wrapped media.Frame = frame
	return &wrapped, nil
}

func (d *syntheticDecoder) Flush() error {
	d.flushed = true
	return nil
}

type syntheticEncoder struct {
	pending *media.Packet
	flushed bool
}

func (e *syntheticEncoder) SendFrame(frame *media.Frame) error {
	audioFrame := (*frame).(*media.AudioFrame)
	packet := media.NewPacket(len(audioFrame.Planes()[0]))
	copy(packet.Data(), audioFrame.Planes()[0])
	e.pending = packet
	return nil
}

func (e *syntheticEncoder) ReceivePacket() (*media.Packet, error) {
	if e.pending != nil {
		packet := e.pending
		e.pending = nil
		return packet, nil
	}
	if e.flushed {
		return nil, engine.ErrEOF
	}
	return nil, engine.ErrEAGAIN
}

func (e *syntheticEncoder) Flush() error {
	e.flushed = true
	return nil
}

type syntheticMuxer struct{ writer io.Writer }

func (*syntheticMuxer) AddStream(media.StreamInfo) (int, error) { return 0, nil }
func (*syntheticMuxer) SetMetadata(metadata.Bundle) error       { return nil }
func (*syntheticMuxer) WriteHeader() error                      { return nil }
func (*syntheticMuxer) WriteTrailer() error                     { return nil }
func (m *syntheticMuxer) WritePacket(_ int, packet *media.Packet) error {
	_, err := m.writer.Write(packet.Data())
	return err
}

func syntheticConfigs(path string, totalBytes int) (DecodeConfig, RoundtripConfig, media.StreamInfo) {
	attrs := media.AudioAttributes{
		SampleRate: 48000, Format: media.SampleFormatS16, BitsPerSample: 16, ChannelLayout: media.LayoutMono1,
	}
	stream := media.StreamInfo{
		Type: media.MediaAudio, MediaAttributes: media.MediaAttributes{Audio: attrs},
	}
	demux := func(io.ReadSeeker) (engine.DemuxerEngine, error) {
		return &syntheticDemuxer{remaining: totalBytes, stream: stream}, nil
	}
	decode := func(media.StreamInfo) engine.DecoderEngine {
		return &syntheticDecoder{attrs: attrs}
	}
	decodeConfig := DecodeConfig{MediaPath: path, StreamInfo: &stream, Demux: demux, Decode: decode}
	roundtripConfig := RoundtripConfig{
		MediaPath:  path,
		StreamInfo: &stream,
		Opts:       pcm.CompareOptions{MaxAbsDiff: 1e-6, MaxRMSE: 1e-6, MinSNR: 100},
		Demux:      demux,
		Decode:     decode,
		Encode: func() engine.EncoderEngine {
			return &syntheticEncoder{}
		},
		Mux: func(writer io.Writer) engine.MuxerEngine {
			return &syntheticMuxer{writer: writer}
		},
	}
	return decodeConfig, roundtripConfig, stream
}

func BenchmarkAudioPipeline(b *testing.B) {
	sizes := []struct {
		name  string
		bytes int
	}{
		{name: "1MiB", bytes: 1 << 20},
		{name: "64MiB", bytes: 64 << 20},
	}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("Decode/%s", size.name), func(b *testing.B) {
			benchmarkAudioPipeline(b, size.bytes, "decode")
		})
		b.Run(fmt.Sprintf("CodecRoundtrip/%s", size.name), func(b *testing.B) {
			benchmarkAudioPipeline(b, size.bytes, "codec")
		})
		b.Run(fmt.Sprintf("FullRoundtrip/%s", size.name), func(b *testing.B) {
			benchmarkAudioPipeline(b, size.bytes, "full")
		})
	}
}

func benchmarkAudioPipeline(b *testing.B, totalBytes int, scenario string) {
	dir := b.TempDir()
	path := dir + string(os.PathSeparator) + "input.bin"
	if err := os.WriteFile(path, nil, 0600); err != nil {
		b.Fatal(err)
	}
	decodeConfig, roundtripConfig, stream := syntheticConfigs(path, totalBytes)
	b.ReportAllocs()
	b.SetBytes(int64(totalBytes))
	b.ResetTimer()
	for b.Loop() {
		var err error
		switch scenario {
		case "decode":
			err = RunDecode(context.Background(), decodeConfig)
		case "codec":
			err = runRoundtripDecodeEncode(context.Background(), roundtripConfig, stream)
		case "full":
			err = runRoundtripFull(context.Background(), nil, dir, roundtripConfig, stream)
		default:
			b.Fatalf("unknown benchmark scenario %q", scenario)
		}
		if err != nil {
			b.Fatal(err)
		}
	}
}
