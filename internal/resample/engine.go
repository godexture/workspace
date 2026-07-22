package resample

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/linear"
	"github.com/godexture/sdk/audio"
	"github.com/godexture/sdk/engine"
)

type Engine struct {
	config config.ResampleConfig
	slot   engine.Slot[media.Frame]

	initialized  bool
	inputRate    int
	layout       media.ChannelLayout
	format       media.SampleFormat
	bits         int
	baseInputPTS media.Pts
	totalInput   int64
	resampler    *linear.Resampler
}

func New(config config.ResampleConfig) (*Engine, error) {
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
	if e.inputRate == e.config.SampleRate {
		input := (*frame).(*media.AudioFrame)
		input.Retain()
		return e.slot.Push(input)
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
	if !e.initialized || e.inputRate == e.config.SampleRate {
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
	e.resampler = linear.NewResampler(e.inputRate, e.config.SampleRate, e.config.SampleRate, block.PTS)
}

func (e *Engine) validateInput(block audio.Block) error {
	if block.Rate != e.inputRate || block.Layout != e.layout || block.Format != e.format || block.Bits != e.bits {
		return fmt.Errorf("resample input format changed within stream")
	}
	if block.PTS != e.baseInputPTS+media.Pts(e.totalInput) {
		return fmt.Errorf("resample input PTS discontinuity: got %d, want %d", block.PTS, e.baseInputPTS+media.Pts(e.totalInput))
	}
	return nil
}
