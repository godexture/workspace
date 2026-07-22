package convert

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/sdk/audio"
)

type Engine struct {
	config config.FormatConfig
	queue  audio.FrameQueue
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
		return e.queue.Push(input)
	}
	block, err := audio.Decode(frame)
	if err != nil {
		return err
	}
	output, err := audio.Encode(block, e.config.Format, e.config.BitsPerSample)
	if err != nil {
		return err
	}
	return e.queue.Push(output)
}

func (e *Engine) ReceiveFrame() (*media.Frame, error) { return e.queue.Receive() }
func (e *Engine) Flush() error                        { e.queue.Flush(); return nil }
func (e *Engine) Close() error                        { e.queue.Close(); return nil }
