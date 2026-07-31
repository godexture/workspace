package remix

import (
	"fmt"
	"math"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/plugins/filter-audio/internal/config"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/buffer"
)

type Engine struct {
	config      config.RemixConfig
	slot        buffer.Slot[media.Frame]
	scratch     audio.Scratch
	mixChannels audio.Channels
}

func New(config config.RemixConfig) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Engine{config: config}, nil
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	block, err := audio.DecodeInto(frame, &e.scratch)
	if err != nil {
		return err
	}
	if block.Layout == e.config.Layout {
		block.Source.Retain()
		return e.slot.Push(block.Source)
	}
	output, err := MixInto(block, e.config, &e.mixChannels)
	if err != nil {
		return err
	}
	encoded, err := audio.EncodeInto(output, block.Format, block.Bits, &e.scratch)
	if err != nil {
		return err
	}
	if err := e.slot.Push(encoded); err != nil {
		encoded.Release()
		return err
	}
	return nil
}

func (e *Engine) ReceiveFrame() (media.Frame, error) {
	return e.slot.Receive()
}
func (e *Engine) Flush() error { e.slot.Flush(); return nil }
func (e *Engine) Close() error { e.slot.Close(); return nil }

// Mix downmixes/upmixes input to config.Layout, always allocating fresh
// output channel buffers. See MixInto for a version that reuses a
// caller-owned scratch buffer across repeated calls.
func Mix(input audio.Block, config config.RemixConfig) (audio.Block, error) {
	var dst audio.Channels
	return MixInto(input, config, &dst)
}

// MixInto behaves like Mix but reuses dst's backing channel buffers across
// calls (grown as needed) instead of allocating fresh ones every time. dst
// must not be read after being passed here until the next call sharing it
// has completed (mixing accumulates into it via +=, so stale reused buffers
// are zeroed first).
func MixInto(input audio.Block, config config.RemixConfig, dst *audio.Channels) (audio.Block, error) {
	if input.Layout.IsAmbisonic() || config.Layout.IsAmbisonic() {
		return audio.Block{}, fmt.Errorf("remix does not support non-identity ambisonic layouts")
	}
	channels := config.Layout.ChannelCount()
	samples := input.Samples()
	if cap(*dst) < channels {
		grown := make(audio.Channels, channels)
		copy(grown, *dst)
		*dst = grown
	} else {
		*dst = (*dst)[:channels]
	}
	for channel := range *dst {
		buf := (*dst)[channel]
		if cap(buf) < samples {
			buf = make([]float32, samples)
		} else {
			buf = buf[:samples]
			clear(buf)
		}
		(*dst)[channel] = buf
	}
	output := audio.Block{
		Channels: *dst,
		Layout:   config.Layout,
		Rate:     input.Rate,
		Format:   input.Format,
		Bits:     input.Bits,
		PTS:      input.PTS,
	}
	if input.Layout.IsUnspecified() || config.Layout.IsUnspecified() {
		for target := range output.Channels {
			if target < len(input.Channels) {
				copy(output.Channels[target], input.Channels[target])
			}
		}
		return output, nil
	}

	for source, values := range input.Channels {
		position := input.Layout.Enumerate()[source]
		if target := config.Layout.Index(position); target >= 0 {
			add(output.Channels[target], values, 1)
			continue
		}
		if err := distribute(output, config, position, values); err != nil {
			return audio.Block{}, err
		}
	}
	if config.Normalize {
		peak := float32(0)
		for _, values := range output.Channels {
			for _, value := range values {
				peak = max(peak, float32(math.Abs(float64(value))))
			}
		}
		if peak > 1 {
			for _, values := range output.Channels {
				for i := range values {
					values[i] /= peak
				}
			}
		}
	}
	return output, nil
}

func distribute(output audio.Block, config config.RemixConfig, position media.ChannelPosition, values []float32) error {
	if len(output.Channels) == 1 {
		gain := float32(1)
		switch position {
		case media.FrontCenter:
			gain = db(config.CenterMixDB)
		case media.LowFrequency:
			gain = db(config.LFEMixDB)
		case media.BackLeft, media.BackRight, media.BackCenter, media.SideLeft, media.SideRight:
			gain = db(config.SurroundMixDB)
		}
		add(output.Channels[0], values, gain)
		return nil
	}
	left, right := output.Layout.Index(media.FrontLeft), output.Layout.Index(media.FrontRight)
	if left < 0 || right < 0 {
		return nil
	}
	gain := float32(1)
	switch position {
	case media.FrontCenter:
		gain = db(config.CenterMixDB)
	case media.LowFrequency:
		gain = db(config.LFEMixDB)
	case media.BackLeft, media.BackRight, media.BackCenter, media.SideLeft, media.SideRight:
		gain = db(config.SurroundMixDB)
	}
	add(output.Channels[left], values, gain)
	add(output.Channels[right], values, gain)
	return nil
}

func add(dst, src []float32, gain float32) {
	for i := range dst {
		dst[i] += src[i] * gain
	}
}

func db(value float64) float32 {
	if math.IsInf(value, -1) {
		return 0
	}
	return float32(math.Pow(10, value/20))
}
