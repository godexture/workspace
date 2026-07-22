package gain

import (
	"fmt"
	"math"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/sdk/audio"
	"github.com/godexture/sdk/engine"
)

type Engine struct {
	factor float32
	slot   engine.Slot[media.Frame]
}

func New(config config.GainConfig) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Engine{factor: float32(math.Pow(10, config.Decibels/20))}, nil
}

func Apply(samples []float32, factor float32) {
	for i := range samples {
		samples[i] *= factor
	}
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if frame == nil || *frame == nil {
		return fmt.Errorf("gain received nil frame")
	}
	input, ok := (*frame).(*media.AudioFrame)
	if !ok {
		return fmt.Errorf("gain expected *media.AudioFrame, got %T", *frame)
	}
	if e.factor == 1 {
		input.Retain()
		return e.slot.Push(input)
	}
	block, err := audio.Decode(frame)
	if err != nil {
		return err
	}
	for _, channel := range block.Channels {
		Apply(channel, e.factor)
	}
	output, err := audio.Encode(block, input.Format, input.BitsPerSample)
	if err != nil {
		return err
	}
	return e.slot.Push(output)
}

func (e *Engine) ReceiveFrame() (*media.Frame, error) {
	frame, err := e.slot.Receive()
	if err != nil {
		return nil, err
	}
	return &frame, nil
}
func (e *Engine) Flush() error { e.slot.Flush(); return nil }
func (e *Engine) Close() error { e.slot.Close(); return nil }
