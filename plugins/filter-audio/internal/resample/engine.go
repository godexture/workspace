package resample

import (
	"fmt"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/plugins/filter-audio/internal/config"
	"github.com/godexture/godec/plugins/filter-audio/internal/linear"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/buffer"
)

type Engine struct {
	config config.ResampleConfig
	slot   buffer.Slot[media.Frame]

	initialized  bool
	inputRate    int
	layout       media.ChannelLayout
	format       media.SampleFormat
	bits         int
	baseInputPTS media.Pts
	totalInput   int64
	resampler    *linear.Resampler
	scratch      audio.Scratch
}

func New(config config.ResampleConfig) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Engine{config: config}, nil
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	block, err := audio.DecodeInto(frame, &e.scratch)
	if err != nil {
		return err
	}
	if !e.initialized {
		e.initialize(block)
	} else if err := e.validateInput(block); err != nil {
		return err
	}
	if e.inputRate == e.config.SampleRate {
		e.totalInput += int64(block.Samples())
		input := (*frame).(*media.AudioFrame)
		input.Retain()
		return e.slot.Push(input)
	}

	output := e.resampler.Process(block)
	e.totalInput += int64(block.Samples())
	if output.Samples() == 0 {
		return nil
	}
	encoded, err := audio.EncodeInto(output, e.format, e.bits, &e.scratch)
	if err != nil {
		return err
	}
	return e.slot.Push(encoded)
}

func (e *Engine) ReceiveFrame() (media.Frame, error) {
	return e.slot.Receive()
}

func (e *Engine) Flush() error {
	if !e.initialized || e.inputRate == e.config.SampleRate {
		e.slot.Flush()
		return nil
	}
	if output, ok := e.resampler.Finish(); ok {
		encoded, err := audio.EncodeInto(output, e.format, e.bits, &e.scratch)
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
