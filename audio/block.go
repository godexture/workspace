package audio

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	mediapcm "github.com/godexture/core/domain/media/pcm"
)

// Channels holds one []float32 sample buffer per audio channel.
type Channels [][]float32

// Clone returns an independent copy that shares no backing arrays with c.
func (c Channels) Clone() Channels {
	result := make(Channels, len(c))
	for i, values := range c {
		result[i] = append([]float32(nil), values...)
	}
	return result
}

type Block struct {
	Source   *media.AudioFrame
	Channels Channels
	Layout   media.ChannelLayout
	Rate     int
	Format   media.SampleFormat
	Bits     int
	PTS      media.Pts
}

func (b Block) Samples() int {
	if len(b.Channels) == 0 {
		return 0
	}
	return len(b.Channels[0])
}

// Slice returns an independent Block covering the [start, end) sample range.
// It never aliases the receiver's Channels, so mutating the result is safe.
func (b Block) Slice(start, end int) Block {
	result := b
	result.PTS += media.Pts(start)
	result.Channels = make(Channels, len(b.Channels))
	for channel, values := range b.Channels {
		result.Channels[channel] = values[start:end]
	}
	return result.Clone()
}

// Clone returns an independent copy that shares no state with b.
func (b Block) Clone() Block {
	clone := b
	clone.Source = nil
	clone.Channels = b.Channels.Clone()
	return clone
}

// Scratch holds reusable per-channel and interleave buffers for repeated
// DecodeInto/EncodeInto calls made by the same caller, so steady-state frame
// processing reuses last frame's backing arrays instead of allocating a
// fresh []float32 per channel per frame. A Block returned by DecodeInto
// aliases its Scratch's buffers, so the caller must be done with it (mutate
// it in place and/or hand it to EncodeInto) before decoding another frame
// into the same Scratch. Not safe for concurrent use: a caller that decodes
// more than one frame concurrently (e.g. a filter with multiple input ports
// pulled on separate goroutines) needs one Scratch per concurrent caller.
type Scratch struct {
	channels    [][]float32
	interleaved []float32
}

func Decode(frame *media.Frame) (Block, error) {
	return DecodeInto(frame, &Scratch{})
}

func DecodeInto(frame *media.Frame, scratch *Scratch) (Block, error) {
	if frame == nil || *frame == nil {
		return Block{}, fmt.Errorf("audio filter received nil frame")
	}
	audioFrame, ok := (*frame).(*media.AudioFrame)
	if !ok {
		return Block{}, fmt.Errorf("audio filter expected *media.AudioFrame, got %T", *frame)
	}
	if err := mediapcm.ValidateFormat(audioFrame.Format); err != nil {
		return Block{}, err
	}
	if err := audioFrame.Layout.Validate(); err != nil {
		return Block{}, fmt.Errorf("invalid input layout: %w", err)
	}
	channels := audioFrame.Layout.ChannelCount()
	if channels == 0 || audioFrame.Samples < 0 || audioFrame.SampleRate <= 0 {
		return Block{}, fmt.Errorf("invalid audio frame properties")
	}
	result := Block{
		Source:   audioFrame,
		Channels: make(Channels, channels),
		Layout:   audioFrame.Layout,
		Rate:     audioFrame.SampleRate,
		Format:   audioFrame.Format,
		Bits:     audioFrame.BitsPerSample,
		PTS:      audioFrame.Pts(),
	}
	if len(scratch.channels) < channels {
		grown := make([][]float32, channels)
		copy(grown, scratch.channels)
		scratch.channels = grown
	}
	if audioFrame.Format.IsPlanar() {
		planes := audioFrame.Planes()
		if len(planes) != channels {
			return Block{}, fmt.Errorf("planar audio has %d planes, want %d", len(planes), channels)
		}
		for channel := range result.Channels {
			decoded, err := mediapcm.ToFloat32(scratch.channels[channel], planes[channel], audioFrame.Format, audioFrame.BitsPerSample)
			if err != nil {
				return Block{}, fmt.Errorf("decode channel %d: %w", channel, err)
			}
			if len(decoded) != audioFrame.Samples {
				return Block{}, fmt.Errorf("channel %d has %d samples, want %d", channel, len(decoded), audioFrame.Samples)
			}
			scratch.channels[channel] = decoded
			result.Channels[channel] = decoded
		}
		return result, nil
	}

	planes := audioFrame.Planes()
	if len(planes) == 0 {
		return Block{}, fmt.Errorf("packed audio has no data plane")
	}
	interleaved, err := mediapcm.ToFloat32(scratch.interleaved, planes[0], audioFrame.Format, audioFrame.BitsPerSample)
	if err != nil {
		return Block{}, err
	}
	scratch.interleaved = interleaved
	if len(interleaved) != channels*audioFrame.Samples {
		return Block{}, fmt.Errorf("packed audio has %d samples, want %d", len(interleaved), channels*audioFrame.Samples)
	}
	for channel := range result.Channels {
		dst := scratch.channels[channel]
		if cap(dst) < audioFrame.Samples {
			dst = make([]float32, audioFrame.Samples)
		} else {
			dst = dst[:audioFrame.Samples]
		}
		for sample := range dst {
			dst[sample] = interleaved[sample*channels+channel]
		}
		scratch.channels[channel] = dst
		result.Channels[channel] = dst
	}
	return result, nil
}

func Encode(block Block, format media.SampleFormat, bitsPerSample int) (*media.AudioFrame, error) {
	return EncodeInto(block, format, bitsPerSample, &Scratch{})
}

func EncodeInto(block Block, format media.SampleFormat, bitsPerSample int, scratch *Scratch) (*media.AudioFrame, error) {
	if err := mediapcm.ValidateFormat(format); err != nil {
		return nil, err
	}
	if err := block.Layout.Validate(); err != nil {
		return nil, fmt.Errorf("invalid output layout: %w", err)
	}
	channels := block.Layout.ChannelCount()
	if len(block.Channels) != channels || channels == 0 || block.Rate <= 0 {
		return nil, fmt.Errorf("invalid audio block")
	}
	samples := block.Samples()
	for channel, values := range block.Channels {
		if len(values) != samples {
			return nil, fmt.Errorf("channel %d has %d samples, want %d", channel, len(values), samples)
		}
	}
	options := []media.AudioFrameOption{media.WithAudioPts(block.PTS)}
	if bitsPerSample > 0 {
		options = append(options, media.WithAudioBitsPerSample(bitsPerSample))
	}
	frame := media.NewAudioFrame(format, block.Layout, block.Rate, samples, options...)
	if format.IsPlanar() {
		for channel, values := range block.Channels {
			if err := mediapcm.FromFloat32(frame.Planes()[channel], values, format, bitsPerSample); err != nil {
				frame.Release()
				return nil, fmt.Errorf("encode channel %d: %w", channel, err)
			}
		}
		return frame, nil
	}

	needed := samples * channels
	interleaved := scratch.interleaved
	if cap(interleaved) < needed {
		interleaved = make([]float32, needed)
	} else {
		interleaved = interleaved[:needed]
	}
	for sample := 0; sample < samples; sample++ {
		for channel := range block.Channels {
			interleaved[sample*channels+channel] = block.Channels[channel][sample]
		}
	}
	scratch.interleaved = interleaved
	if err := mediapcm.FromFloat32(frame.Planes()[0], interleaved, format, bitsPerSample); err != nil {
		frame.Release()
		return nil, err
	}
	return frame, nil
}
