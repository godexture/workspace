package gate

import (
	"fmt"
	"math"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/audio"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/framequeue"
)

type Engine struct {
	threshold float32
	queue     framequeue.Single
}

func New(cfg config.GateConfig) (*Engine, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Engine{threshold: float32(math.Pow(10, cfg.ThresholdDBFS/20))}, nil
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if frame == nil || *frame == nil {
		return fmt.Errorf("gate received nil frame")
	}
	input, ok := (*frame).(*media.AudioFrame)
	if !ok {
		return fmt.Errorf("gate expected *media.AudioFrame, got %T", *frame)
	}
	block, err := audio.Decode(frame)
	if err != nil {
		return err
	}
	// A sample index is silenced only when every channel is below the
	// threshold there, so multi-channel audio never desyncs across channels.
	for i := 0; i < block.Samples(); i++ {
		active := false
		for _, values := range block.Channels {
			if float32(math.Abs(float64(values[i]))) >= e.threshold {
				active = true
				break
			}
		}
		if !active {
			for _, values := range block.Channels {
				values[i] = 0
			}
		}
	}
	output, err := audio.Encode(block, input.Format, input.BitsPerSample)
	if err != nil {
		return err
	}
	return e.queue.Push(output)
}

func (e *Engine) ReceiveFrame() (*media.Frame, error) { return e.queue.Receive() }
func (e *Engine) Flush() error                        { e.queue.Flush(); return nil }
func (e *Engine) Close() error                        { e.queue.Close(); return nil }
