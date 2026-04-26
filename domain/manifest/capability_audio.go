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
	reasonInvalidProfile
	reasonSampleRate
	reasonChannel
	reasonLayout
	reasonFormat
)

type AudioConstraint struct {
	SampleRates []int
	Channels    []int
	Layouts     []media.ChannelLayout
	Formats     []media.SampleFormat
}

func (c *AudioConstraint) check(p media.Profile) rejectReason {
	if p.Type != media.MediaAudio {
		return reasonTypeMismatch
	}

	attrs := p.Audio

	if !slices.Contains(c.SampleRates, attrs.SampleRate) {
		return reasonSampleRate
	}

	if !slices.Contains(c.Channels, attrs.ChannelCount()) {
		return reasonChannel
	}

	if !slices.Contains(c.Formats, attrs.Format) {
		return reasonFormat
	}

	if !slices.ContainsFunc(c.Layouts, func(l media.ChannelLayout) bool {
		return l == attrs.ChannelLayout
	}) {
		return reasonLayout
	}

	return reasonNone
}

func (c *AudioConstraint) Match(p media.Profile) bool {
	return c.check(p) == reasonNone
}

func (c *AudioConstraint) Diagnose(p media.Profile) error {
	code := c.check(p)

	switch code {
	case reasonNone:
		return nil

	case reasonTypeMismatch:
		return fmt.Errorf("type mismatch: expected audio, got %s", p.Type)

	case reasonInvalidProfile:
		return fmt.Errorf("internal error: profile is not AudioProfile")

	case reasonSampleRate:
		return fmt.Errorf("unsupported sample rate: %d Hz (allowed: %v)",
			p.Audio.SampleRate, c.SampleRates)

	case reasonChannel:
		return fmt.Errorf("unsupported channel: %d ch. (allowed: %v)",
			p.Audio.ChannelCount(), c.Channels)

	case reasonLayout:
		return fmt.Errorf("unsupported channel layout: %s (allowed: %v)",
			p.Audio.ChannelLayout.String(), c.Layouts)

	default:
		return fmt.Errorf("unknown constraint violation")
	}
}
