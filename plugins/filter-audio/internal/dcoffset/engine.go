package dcoffset

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/sdk/audio"
	"github.com/godexture/sdk/buffer"
)

type Engine struct {
	pole    float32
	lastX   []float32
	lastY   []float32
	slot    buffer.Slot[media.Frame]
	scratch audio.Scratch
}

func New(config config.DCOffsetConfig) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Engine{pole: float32(config.Pole)}, nil
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if frame == nil || *frame == nil {
		return fmt.Errorf("DC offset filter received nil frame")
	}
	input, ok := (*frame).(*media.AudioFrame)
	if !ok {
		return fmt.Errorf("DC offset filter expected *media.AudioFrame, got %T", *frame)
	}
	block, err := audio.DecodeInto(frame, &e.scratch)
	if err != nil {
		return err
	}
	if len(e.lastX) != len(block.Channels) {
		e.lastX = make([]float32, len(block.Channels))
		e.lastY = make([]float32, len(block.Channels))
	}
	for channel, values := range block.Channels {
		for i, value := range values {
			output := value - e.lastX[channel] + e.pole*e.lastY[channel]
			e.lastX[channel] = value
			e.lastY[channel] = output
			values[i] = output
		}
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
