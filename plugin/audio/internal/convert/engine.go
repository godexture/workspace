package convert

import (
	"fmt"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/buffer"
)

type Engine struct {
	config  config.FormatConfig
	slot    buffer.Slot[media.Frame]
	scratch audio.Scratch
}

func New(config config.FormatConfig) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Engine{config: config}, nil
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if frame == nil || *frame == nil {
		return fmt.Errorf("convert received nil frame")
	}
	input, ok := (*frame).(*media.AudioFrame)
	if !ok {
		return fmt.Errorf("convert expected *media.AudioFrame, got %T", *frame)
	}
	bits := e.config.EffectiveBitsPerSample()
	if input.Format == e.config.Format && input.BitsPerSample == bits {
		input.Retain()
		return e.slot.Push(input)
	}
	block, err := audio.DecodeInto(frame, &e.scratch)
	if err != nil {
		return err
	}
	output, err := audio.EncodeInto(block, e.config.Format, e.config.BitsPerSample, &e.scratch)
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
