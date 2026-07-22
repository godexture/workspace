package remix

import (
	"fmt"
	"math"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/sdk/audio"
)

type Engine struct {
	config config.RemixConfig
	queue  audio.FrameQueue
}

func New(config config.RemixConfig) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Engine{config: config}, nil
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	block, err := audio.Decode(frame)
	if err != nil {
		return err
	}
	if block.Layout == e.config.Layout {
		block.Source.Retain()
		return e.queue.Push(block.Source)
	}
	output, err := Mix(block, e.config)
	if err != nil {
		return err
	}
	encoded, err := audio.Encode(output, block.Format, block.Bits)
	if err != nil {
		return err
	}
	if err := e.queue.Push(encoded); err != nil {
		encoded.Release()
		return err
	}
	return nil
}

func (e *Engine) ReceiveFrame() (*media.Frame, error) { return e.queue.Receive() }
func (e *Engine) Flush() error                        { e.queue.Flush(); return nil }
func (e *Engine) Close() error                        { e.queue.Close(); return nil }

func Mix(input audio.Block, config config.RemixConfig) (audio.Block, error) {
	if input.Layout.IsAmbisonic() || config.Layout.IsAmbisonic() {
		return audio.Block{}, fmt.Errorf("remix does not support non-identity ambisonic layouts")
	}
	output := audio.Block{
		Channels: make([][]float32, config.Layout.ChannelCount()),
		Layout:   config.Layout,
		Rate:     input.Rate,
		PTS:      input.PTS,
		Metadata: input.Metadata,
	}
	for channel := range output.Channels {
		output.Channels[channel] = make([]float32, input.Samples())
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
