package gain

import (
	"fmt"
	"math"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/buffer"
)

type Engine struct {
	factor  float32
	slot    buffer.Slot[media.Frame]
	scratch audio.Scratch
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
	block, err := audio.DecodeInto(frame, &e.scratch)
	if err != nil {
		return err
	}
	for _, channel := range block.Channels {
		Apply(channel, e.factor)
	}
	output, err := audio.EncodeInto(block, input.Format, input.BitsPerSample, &e.scratch)
	if err != nil {
		return err
	}
	return e.slot.Push(output)
}

func (e *Engine) ReceiveFrame() (media.Frame, error) {
	return e.slot.Receive()
}
func (e *Engine) Flush() error { e.slot.Flush(); return nil }
func (e *Engine) Close() error { e.slot.Close(); return nil }
