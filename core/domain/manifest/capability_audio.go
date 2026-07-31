package manifest

import (
	"fmt"
	"slices"

	"github.com/godexture/core/domain/media"
)

type rejectReason uint8

const (
	reasonNone rejectReason = iota
	reasonTypeMismatch
	reasonCodec
	reasonSampleRate
	reasonChannel
	reasonLayout
	reasonFormat
)

type AudioConstraint struct {
	Codecs        []media.CodecID
	SampleRates   IntConstraint
	Channels      IntConstraint
	Layouts       []media.ChannelLayout
	SampleFormats []SampleFormatConstraint
}

func (c *AudioConstraint) check(stream media.StreamInfo) rejectReason {
	if stream.Type != media.MediaAudio {
		return reasonTypeMismatch
	}

	attrs := stream.Audio
	if len(c.Codecs) > 0 && !slices.Contains(c.Codecs, stream.Codec) {
		return reasonCodec
	}

	if !c.SampleRates.Match(attrs.SampleRate) {
		return reasonSampleRate
	}

	if !c.Channels.Match(attrs.ChannelCount()) {
		return reasonChannel
	}

	if !c.matchesFormat(attrs) {
		return reasonFormat
	}

	if len(c.Layouts) > 0 && !slices.ContainsFunc(c.Layouts, func(l media.ChannelLayout) bool {
		return l == attrs.ChannelLayout
	}) {
		return reasonLayout
	}

	return reasonNone
}

func (c *AudioConstraint) matchesFormat(attrs media.AudioAttributes) bool {
	if len(c.SampleFormats) == 0 {
		return true
	}
	bits := attrs.EffectiveBitsPerSample()
	return slices.ContainsFunc(c.SampleFormats, func(candidate SampleFormatConstraint) bool {
		return candidate.Format == attrs.Format && candidate.BitsPerSample.Match(bits)
	})
}

func (c *AudioConstraint) Match(stream media.StreamInfo) bool {
	return c.check(stream) == reasonNone
}

func (c *AudioConstraint) Diagnose(stream media.StreamInfo) error {
	code := c.check(stream)

	switch code {
	case reasonNone:
		return nil

	case reasonTypeMismatch:
		return fmt.Errorf("type mismatch: expected audio, got %s", stream.Type)

	case reasonCodec:
		return fmt.Errorf("unsupported codec: %s (allowed: %v)", stream.Codec, c.Codecs)

	case reasonSampleRate:
		return fmt.Errorf("unsupported sample rate: %d Hz (allowed: %v)",
			stream.Audio.SampleRate, c.SampleRates)

	case reasonChannel:
		return fmt.Errorf("unsupported channel: %d ch. (allowed: %v)",
			stream.Audio.ChannelCount(), c.Channels)

	case reasonLayout:
		return fmt.Errorf("unsupported channel layout: %s (allowed: %v)",
			stream.Audio.ChannelLayout.String(), c.Layouts)

	case reasonFormat:
		bits := stream.Audio.EffectiveBitsPerSample()
		return fmt.Errorf("unsupported sample format: %s/%d bits (allowed: %v)",
			stream.Audio.Format, bits, c.SampleFormats)

	default:
		return fmt.Errorf("unknown constraint violation")
	}
}
