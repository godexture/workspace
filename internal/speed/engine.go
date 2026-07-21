package speed

import (
	"fmt"
	"math"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/audio"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/framequeue"
	"github.com/godexture/filter-audio/internal/linear"
)

type Engine struct {
	config config.SpeedConfig
	queue  framequeue.Single

	initialized  bool
	inputRate    int
	layout       media.ChannelLayout
	format       media.SampleFormat
	bits         int
	baseInputPTS media.Pts
	totalInput   int64
	resampler    *linear.Resampler
}

func New(config config.SpeedConfig) (*Engine, error) {
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
	if !e.initialized {
		e.initialize(block)
	} else if err := e.validateInput(block); err != nil {
		return err
	}
	if e.config.Factor == 1 {
		input := (*frame).(*media.AudioFrame)
		input.Retain()
		return e.queue.Push(input)
	}
	output := e.resampler.Process(block)
	e.totalInput += int64(block.Samples())
	if output.Samples() == 0 {
		return nil
	}
	encoded, err := audio.Encode(output, e.format, e.bits)
	if err != nil {
		return err
	}
	return e.queue.Push(encoded)
}

func (e *Engine) ReceiveFrame() (*media.Frame, error) { return e.queue.Receive() }

func (e *Engine) Flush() error {
	if !e.initialized || e.config.Factor == 1 {
		e.queue.Flush()
		return nil
	}
	if output, ok := e.resampler.Finish(); ok {
		encoded, err := audio.Encode(output, e.format, e.bits)
		if err != nil {
			return err
		}
		if err := e.queue.Push(encoded); err != nil {
			encoded.Release()
			return err
		}
	}
	e.queue.Flush()
	return nil
}

func (e *Engine) Close() error {
	e.queue.Close()
	return nil
}

func (e *Engine) initialize(block audio.Block) {
	e.initialized = true
	e.inputRate = block.Rate
	e.layout = block.Layout
	e.format = block.Format
	e.bits = block.Bits
	e.baseInputPTS = block.PTS
	target := int(math.Round(float64(block.Rate) / e.config.Factor))
	if target < 1 {
		target = 1
	}
	e.resampler = linear.NewResampler(e.inputRate, target, e.inputRate, block.PTS)
}

func (e *Engine) validateInput(block audio.Block) error {
	if block.Rate != e.inputRate || block.Layout != e.layout || block.Format != e.format || block.Bits != e.bits {
		return fmt.Errorf("speed input format changed within stream")
	}
	if block.PTS != e.baseInputPTS+media.Pts(e.totalInput) {
		return fmt.Errorf("speed input PTS discontinuity: got %d, want %d", block.PTS, e.baseInputPTS+media.Pts(e.totalInput))
	}
	return nil
}
