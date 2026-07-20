package audio

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	mediapcm "github.com/godexture/core/domain/media/pcm"
	"github.com/godexture/core/domain/metadata"
)

type Block struct {
	Channels [][]float32
	Layout   media.ChannelLayout
	Rate     int
	PTS      media.Pts
	Metadata *metadata.Bundle
}

func (b Block) Samples() int {
	if len(b.Channels) == 0 {
		return 0
	}
	return len(b.Channels[0])
}

func Decode(frame *media.Frame) (Block, error) {
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
		Channels: make([][]float32, channels),
		Layout:   audioFrame.Layout,
		Rate:     audioFrame.SampleRate,
		PTS:      audioFrame.Pts(),
		Metadata: audioFrame.Metadata(),
	}
	if audioFrame.Format.IsPlanar() {
		planes := audioFrame.Planes()
		if len(planes) != channels {
			return Block{}, fmt.Errorf("planar audio has %d planes, want %d", len(planes), channels)
		}
		for channel := range result.Channels {
			decoded, err := mediapcm.ToFloat32(nil, planes[channel], audioFrame.Format, audioFrame.BitsPerSample)
			if err != nil {
				return Block{}, fmt.Errorf("decode channel %d: %w", channel, err)
			}
			if len(decoded) != audioFrame.Samples {
				return Block{}, fmt.Errorf("channel %d has %d samples, want %d", channel, len(decoded), audioFrame.Samples)
			}
			result.Channels[channel] = decoded
		}
		return result, nil
	}

	planes := audioFrame.Planes()
	if len(planes) == 0 {
		return Block{}, fmt.Errorf("packed audio has no data plane")
	}
	interleaved, err := mediapcm.ToFloat32(nil, planes[0], audioFrame.Format, audioFrame.BitsPerSample)
	if err != nil {
		return Block{}, err
	}
	if len(interleaved) != channels*audioFrame.Samples {
		return Block{}, fmt.Errorf("packed audio has %d samples, want %d", len(interleaved), channels*audioFrame.Samples)
	}
	for channel := range result.Channels {
		result.Channels[channel] = make([]float32, audioFrame.Samples)
		for sample := range result.Channels[channel] {
			result.Channels[channel][sample] = interleaved[sample*channels+channel]
		}
	}
	return result, nil
}

func Encode(block Block, format media.SampleFormat, bitsPerSample int) (*media.AudioFrame, error) {
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
	if block.Metadata != nil {
		frame.Metadata().Merge(block.Metadata)
	}
	if format.IsPlanar() {
		for channel, values := range block.Channels {
			if err := mediapcm.FromFloat32(frame.Planes()[channel], values, format, bitsPerSample); err != nil {
				frame.Release()
				return nil, fmt.Errorf("encode channel %d: %w", channel, err)
			}
		}
		return frame, nil
	}

	interleaved := make([]float32, samples*channels)
	for sample := 0; sample < samples; sample++ {
		for channel := range block.Channels {
			interleaved[sample*channels+channel] = block.Channels[channel][sample]
		}
	}
	if err := mediapcm.FromFloat32(frame.Planes()[0], interleaved, format, bitsPerSample); err != nil {
		frame.Release()
		return nil, err
	}
	return frame, nil
}

func CloneChannels(channels [][]float32) [][]float32 {
	result := make([][]float32, len(channels))
	for i, values := range channels {
		result[i] = append([]float32(nil), values...)
	}
	return result
}

func CloneBlock(block Block) Block {
	clone := block
	clone.Channels = CloneChannels(block.Channels)
	if block.Metadata != nil {
		clone.Metadata = block.Metadata.Clone()
	}
	return clone
}
