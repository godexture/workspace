package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/sdk/engine"
)

// DownmixToMono downmixes a stereo float32 PCM slice to mono.
func DownmixToMono(stereo []float32) []float32 {
	mono := make([]float32, len(stereo)/2)
	for i := 0; i < len(mono); i++ {
		mono[i] = (stereo[i*2] + stereo[i*2+1]) * 0.5
	}
	return mono
}

// Resample16kTo8k downsamples a 16kHz float32 PCM slice to 8kHz by simple decimation.
func Resample16kTo8k(in []float32, channels int) []float32 {
	out := make([]float32, len(in)/2)
	for i := 0; i < len(out)/channels; i++ {
		for c := 0; c < channels; c++ {
			out[i*channels+c] = in[i*2*channels+c]
		}
	}
	return out
}

// ConvertToFloat32 converts an AudioFrame's samples to a float32 slice.
func ConvertToFloat32(af *media.AudioFrame) ([]float32, error) {
	plane := af.Planes()[0]
	channels := af.Layout.ChannelCount()
	samples := af.Samples
	totalSamples := samples * channels
	bitsPerSample := af.BitsPerSample
	if bitsPerSample <= 0 {
		bitsPerSample = af.Format.BytesPerSample() * 8
	}

	pcm := make([]float32, totalSamples)
	switch af.Format {
	case media.SampleFormatU8:
		for i := 0; i < totalSamples; i++ {
			pcm[i] = (float32(plane[i]) - 128.0) / 128.0
		}
	case media.SampleFormatS16:
		scale := float32(uint64(1) << uint(bitsPerSample-1))
		for i := 0; i < totalSamples; i++ {
			val := int16(binary.LittleEndian.Uint16(plane[i*2 : (i+1)*2]))
			pcm[i] = float32(val) / scale
		}
	case media.SampleFormatS24:
		scale := float32(uint64(1) << uint(bitsPerSample-1))
		for i := 0; i < totalSamples; i++ {
			offset := i * 3
			value := int32(uint32(plane[offset]) | uint32(plane[offset+1])<<8 | uint32(plane[offset+2])<<16)
			if value&0x800000 != 0 {
				value |= ^int32(0xffffff)
			}
			pcm[i] = float32(value) / scale
		}
	case media.SampleFormatS32:
		scale := float32(uint64(1) << uint(bitsPerSample-1))
		for i := 0; i < totalSamples; i++ {
			val := int32(binary.LittleEndian.Uint32(plane[i*4 : (i+1)*4]))
			pcm[i] = float32(val) / scale
		}
	case media.SampleFormatF32:
		for i := 0; i < totalSamples; i++ {
			bits := binary.LittleEndian.Uint32(plane[i*4 : (i+1)*4])
			pcm[i] = math.Float32frombits(bits)
		}
	default:
		return nil, fmt.Errorf("unsupported sample format in conversion: %v", af.Format)
	}
	return pcm, nil
}

// DecodeToFloat32 decodes all packets from the demuxer using the decoder and returns them as a float32 PCM slice.
// It uses pipeline nodes and a runner internally to avoid writing manual engine loops.
func DecodeToFloat32(ctx context.Context, demuxEngine engine.DemuxerEngine, decodeEngine engine.DecoderEngine) ([]float32, error) {
	demuxerNode := engine.WrapDemuxer(demuxEngine)
	decoderNode := engine.WrapDecoder(decodeEngine)
	collector := NewPCMCollectorNode()

	if err := pipeline.LinkAny(demuxerNode, "out", decoderNode, "in"); err != nil {
		return nil, err
	}
	if err := pipeline.LinkAny(decoderNode, "out", collector, "in"); err != nil {
		return nil, err
	}

	runner := pipeline.NewRunner()
	if err := runner.Run(ctx, []node.Node{demuxerNode, decoderNode, collector}); err != nil {
		return nil, err
	}

	return collector.PCM(), nil
}

// EncodeToMuxer encodes the given float32 PCM array into the muxer using the specified encoder.
// It uses pipeline nodes and a runner internally.
func EncodeToMuxer(ctx context.Context, encode engine.EncoderEngine, mux engine.MuxerEngine, srcPCM []float32, attrs media.AudioAttributes) error {
	generator := NewPCMGeneratorNode(srcPCM, attrs)
	encoder := engine.WrapEncoder(encode)
	muxer := engine.WrapMuxer(mux)

	if err := pipeline.LinkAny(generator, "out", encoder, "in"); err != nil {
		return err
	}
	if err := pipeline.LinkAny(encoder, "out", muxer, "in"); err != nil {
		return err
	}

	runner := pipeline.NewRunner()
	return runner.Run(ctx, []node.Node{generator, encoder, muxer})
}
