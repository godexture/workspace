package retime

import (
	"fmt"
	"math"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/linear"
	"github.com/godexture/sdk/audio"
	"github.com/godexture/sdk/buffer"
)

type Engine struct {
	config config.SpeedConfig
	slot   buffer.Slot[media.Frame]

	initialized   bool
	inputRate     int
	layout        media.ChannelLayout
	format        media.SampleFormat
	bits          int
	baseInputPTS  media.Pts
	totalInput    int64
	outputRate    int
	outputBasePTS media.Pts
	resampler     *linear.Resampler
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
		return e.slot.Push(input)
	}

	if e.config.Mode == config.SpeedModeRelabel {
		output := block
		output.Rate = e.outputRate
		output.PTS = e.outputBasePTS + media.Pts(e.totalInput)
		e.totalInput += int64(block.Samples())
		encoded, err := audio.Encode(output, e.format, e.bits)
		if err != nil {
			return err
		}
		return e.slot.Push(encoded)
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
	return e.slot.Push(encoded)
}

func (e *Engine) ReceiveFrame() (*media.Frame, error) {
	frame, err := e.slot.Receive()
	if err != nil {
		return nil, err
	}
	return &frame, nil
}

func (e *Engine) Flush() error {
	if !e.initialized || e.config.Factor == 1 || e.config.Mode == config.SpeedModeRelabel {
		e.slot.Flush()
		return nil
	}
	if output, ok := e.resampler.Finish(); ok {
		encoded, err := audio.Encode(output, e.format, e.bits)
		if err != nil {
			return err
		}
		if err := e.slot.Push(encoded); err != nil {
			encoded.Release()
			return err
		}
	}
	e.slot.Flush()
	return nil
}

func (e *Engine) Close() error {
	e.slot.Close()
	return nil
}

func (e *Engine) initialize(block audio.Block) {
	e.initialized = true
	e.inputRate = block.Rate
	e.layout = block.Layout
	e.format = block.Format
	e.bits = block.Bits
	e.baseInputPTS = block.PTS
	if e.config.Mode == config.SpeedModeRelabel {
		e.outputRate = scaleRate(block.Rate, e.config.Factor)
		e.outputBasePTS = linear.RescalePTS(block.PTS, e.inputRate, e.outputRate)
		return
	}
	target := scaleRate(block.Rate, 1/e.config.Factor)
	e.resampler = linear.NewResampler(e.inputRate, target, e.inputRate, block.PTS)
}

func (e *Engine) validateInput(block audio.Block) error {
	if block.Rate != e.inputRate || block.Layout != e.layout || block.Format != e.format || block.Bits != e.bits {
		return fmt.Errorf("retime input format changed within stream")
	}
	if block.PTS != e.baseInputPTS+media.Pts(e.totalInput) {
		return fmt.Errorf("retime input PTS discontinuity: got %d, want %d", block.PTS, e.baseInputPTS+media.Pts(e.totalInput))
	}
	return nil
}

func scaleRate(rate int, factor float64) int {
	target := int(math.Round(float64(rate) * factor))
	if target < 1 {
		target = 1
	}
	return target
}
