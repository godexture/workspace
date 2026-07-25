package filter

import (
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/registry"
)

func bridgeFormat(current media.StreamInfo, required []manifest.Capability) ([]registry.ConversionCandidate, error) {
	var result []registry.ConversionCandidate
	currentBits := current.Audio.EffectiveBitsPerSample()
	for _, constraint := range audioConstraints(required) {
		for _, target := range constraint.SampleFormats {
			bits, ok := preferredBits(target, currentBits)
			if !ok {
				continue
			}
			if target.Format != current.Audio.Format || bits != currentBits {
				result = append(result, registry.ConversionCandidate{
					Config: NewFormatConfig(WithFormat(target.Format), WithBitsPerSample(bits)),
					Cost:   registry.ConversionCost{QualityLoss: formatLoss(current.Audio.Format, target.Format, currentBits, bits), Work: 1},
				})
			}
		}
	}
	return result, nil
}

func bridgeRate(current media.StreamInfo, required []manifest.Capability) ([]registry.ConversionCandidate, error) {
	var result []registry.ConversionCandidate
	for _, constraint := range audioConstraints(required) {
		for _, rate := range constraint.SampleRates.Candidates(current.Audio.SampleRate) {
			if rate != current.Audio.SampleRate {
				result = append(result, registry.ConversionCandidate{Config: NewResampleConfig(WithSampleRate(rate)), Cost: registry.ConversionCost{QualityLoss: 1, Work: 2}})
			}
		}
	}
	return result, nil
}

func bridgeLayout(current media.StreamInfo, required []manifest.Capability) ([]registry.ConversionCandidate, error) {
	var result []registry.ConversionCandidate
	for _, constraint := range audioConstraints(required) {
		for _, layout := range constraint.Layouts {
			if layout != current.Audio.ChannelLayout {
				result = append(result, registry.ConversionCandidate{Config: NewRemixConfig(WithLayout(layout)), Cost: registry.ConversionCost{QualityLoss: 1, Work: 2}})
			}
		}
		for _, channels := range constraint.Channels.Candidates(current.Audio.ChannelCount()) {
			if channels != current.Audio.ChannelCount() {
				result = append(result, registry.ConversionCandidate{Config: NewRemixConfig(WithLayout(layoutForChannels(channels))), Cost: registry.ConversionCost{QualityLoss: 1, Work: 2}})
			}
		}
	}
	return result, nil
}

func audioConstraints(required []manifest.Capability) []*manifest.AudioConstraint {
	constraints := make([]*manifest.AudioConstraint, 0, len(required))
	for _, capability := range required {
		if constraint, ok := capability.(*manifest.AudioConstraint); ok {
			constraints = append(constraints, constraint)
		}
	}
	return constraints
}

func preferredBits(target manifest.SampleFormatConstraint, current int) (int, bool) {
	constraint := target.BitsPerSample
	if len(constraint.Values) > 0 {
		if constraint.Match(current) {
			return current, true
		}
		for _, bits := range constraint.Candidates(current) {
			return bits, true
		}
		return 0, false
	}
	bits := target.Format.BitsPerSample()
	if constraint.Min != 0 && bits < constraint.Min {
		bits = constraint.Min
	}
	if constraint.Max != 0 && bits > constraint.Max {
		bits = constraint.Max
	}
	return bits, constraint.Match(bits)
}

func formatLoss(from, to media.SampleFormat, fromBits, toBits int) uint32 {
	if isFloat(from) && !isFloat(to) {
		return 1
	}
	if !isFloat(from) && !isFloat(to) && toBits < fromBits {
		return 1
	}
	return 0
}

func isFloat(format media.SampleFormat) bool {
	return format.Packed() == media.SampleFormatF32 || format.Packed() == media.SampleFormatF64
}

func layoutForChannels(channels int) media.ChannelLayout {
	switch channels {
	case 1:
		return media.LayoutMono1
	case 2:
		return media.LayoutStereo2_0
	case 6:
		return media.LayoutSide5_1
	case 8:
		return media.LayoutSurround7_1
	default:
		return media.NewUnspecified(channels)
	}
}
