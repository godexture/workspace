package audio

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil/audio/nodes"
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
		media.WithAudioBitsPerSample(d.attrs.EffectiveBitsPerSample()))
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

func BenchmarkAudioPipelineObservation(b *testing.B) {
	for _, size := range []struct {
		name  string
		bytes int
	}{{name: "1MiB", bytes: 1 << 20}, {name: "64MiB", bytes: 64 << 20}} {
		for _, variant := range []struct {
			name  string
			plain bool
			mode  pipeline.ObservationMode
		}{
			{name: "Plain", plain: true},
			{name: "ObservationOff", mode: pipeline.ObservationOff},
			{name: "ObservationProgress", mode: pipeline.ObservationProgress},
			{name: "ObservationMetrics", mode: pipeline.ObservationMetrics},
		} {
			b.Run(fmt.Sprintf("%s/%s", size.name, variant.name), func(b *testing.B) {
				benchmarkAudioPipelineObservation(b, size.bytes, variant.plain, variant.mode)
			})
		}
	}
}

func BenchmarkAudioPipelineObservationOffProfile(b *testing.B) {
	benchmarkAudioPipelineObservation(b, 64<<20, false, pipeline.ObservationOff)
}

func BenchmarkAudioPipelineObservationProgressProfile(b *testing.B) {
	benchmarkAudioPipelineObservation(b, 64<<20, false, pipeline.ObservationProgress)
}

func BenchmarkAudioPipelineObservationMetricsProfile(b *testing.B) {
	benchmarkAudioPipelineObservation(b, 64<<20, false, pipeline.ObservationMetrics)
}

func benchmarkAudioPipelineObservation(b *testing.B, totalBytes int, plain bool, mode pipeline.ObservationMode) {
	dir := b.TempDir()
	path := dir + string(os.PathSeparator) + "input.bin"
	if err := os.WriteFile(path, nil, 0600); err != nil {
		b.Fatal(err)
	}
	decodeConfig, _, _ := syntheticConfigs(path, totalBytes)
	b.ReportAllocs()
	b.SetBytes(int64(totalBytes))
	b.ResetTimer()
	for b.Loop() {
		var err error
		if plain {
			err = runPlainDecode(context.Background(), decodeConfig)
		} else {
			err = runObservedDecode(context.Background(), decodeConfig, mode)
		}
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAudioPipelineObservationPaired64MiB(b *testing.B) {
	const totalBytes = 64 << 20
	dir := b.TempDir()
	path := dir + string(os.PathSeparator) + "input.bin"
	if err := os.WriteFile(path, nil, 0600); err != nil {
		b.Fatal(err)
	}
	decodeConfig, _, _ := syntheticConfigs(path, totalBytes)
	type pair struct {
		first  string
		second string
		ratios []float64
	}
	pairs := []pair{
		{first: "plain", second: "off"},
		{first: "off", second: "progress"},
		{first: "off", second: "metrics"},
	}
	sample := 0
	for b.Loop() {
		for i := range pairs {
			first, second := pairs[i].first, pairs[i].second
			if sample%2 == 1 {
				first, second = second, first
			}
			firstDuration := benchmarkAudioObservationVariant(b, decodeConfig, first)
			secondDuration := benchmarkAudioObservationVariant(b, decodeConfig, second)
			durations := map[string]time.Duration{first: firstDuration, second: secondDuration}
			pairs[i].ratios = append(pairs[i].ratios,
				float64(durations[pairs[i].second])/float64(durations[pairs[i].first]))
		}
		sample++
	}
	b.ReportMetric((audioMedianRatio(pairs[0].ratios)-1)*100, "off-vs-plain-%")
	b.ReportMetric((audioMedianRatio(pairs[1].ratios)-1)*100, "progress-vs-off-%")
	b.ReportMetric((audioMedianRatio(pairs[2].ratios)-1)*100, "metrics-vs-off-%")
}

func benchmarkAudioObservationVariant(b *testing.B, config DecodeConfig, variant string) time.Duration {
	b.Helper()
	runtime.GC()
	started := time.Now()
	var err error
	switch variant {
	case "plain":
		err = runPlainDecode(context.Background(), config)
	case "off":
		err = runObservedDecode(context.Background(), config, pipeline.ObservationOff)
	case "progress":
		err = runObservedDecode(context.Background(), config, pipeline.ObservationProgress)
	case "metrics":
		err = runObservedDecode(context.Background(), config, pipeline.ObservationMetrics)
	default:
		b.Fatalf("unknown observation variant %q", variant)
	}
	if err != nil {
		b.Fatal(err)
	}
	return time.Since(started)
}

func audioMedianRatio(values []float64) float64 {
	values = append([]float64(nil), values...)
	sort.Float64s(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}

func runPlainDecode(ctx context.Context, cfg DecodeConfig) error {
	demux, err := openDemuxNode(cfg.MediaPath, cfg.Demux, cfg.StreamInfo)
	if err != nil {
		return err
	}
	defer demux.close()
	decoder := engine.WrapDecoder(cfg.Decode(demux.stream))
	discard := nodes.NewFrameDiscard()
	if err := pipeline.Link(demux.node, "out", decoder, "in"); err != nil {
		return err
	}
	if err := pipeline.Link(decoder, "out", discard, "in"); err != nil {
		return err
	}
	conversion, err := pipeline.New(demux.node, decoder, discard)
	if err != nil {
		return err
	}
	return conversion.Run(ctx)
}

func runObservedDecode(ctx context.Context, cfg DecodeConfig, mode pipeline.ObservationMode) error {
	demux, err := openDemuxNode(cfg.MediaPath, cfg.Demux, cfg.StreamInfo)
	if err != nil {
		return err
	}
	defer demux.close()
	decoder := engine.WrapDecoder(cfg.Decode(demux.stream))
	discard := nodes.NewFrameDiscard()
	geometry := pipeline.NewGeometry()
	defer geometry.Close()
	definitions := []pipeline.NodeDef{
		{ID: "demuxer", Node: demux.node, Description: pipeline.NodeDescription{Role: manifest.RoleDemuxer, Outputs: []media.StreamInfo{demux.stream}}},
		{ID: "decoder", Node: decoder, Description: pipeline.NodeDescription{Role: manifest.RoleDecoder, Inputs: []media.StreamInfo{demux.stream}, Outputs: []media.StreamInfo{demux.stream}}},
		{ID: "discard", Node: discard, Description: pipeline.NodeDescription{Role: manifest.RoleUnknown, Inputs: []media.StreamInfo{demux.stream}}},
	}
	for _, definition := range definitions {
		if err := geometry.AddNodeDef(definition); err != nil {
			return err
		}
	}
	edges := []pipeline.EdgeDef{
		{FromNode: "demuxer", FromPort: "out", ToNode: "decoder", ToPort: "in", Stream: demux.stream, ProgressSource: true},
		{FromNode: "decoder", FromPort: "out", ToNode: "discard", ToPort: "in", Stream: demux.stream},
	}
	for _, edge := range edges {
		if err := geometry.AddEdgeDef(edge); err != nil {
			return err
		}
	}
	conversion, err := pipeline.NewBuilder().Build(geometry, pipeline.WithObservation(mode))
	if err != nil {
		return err
	}
	return conversion.Run(ctx)
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
