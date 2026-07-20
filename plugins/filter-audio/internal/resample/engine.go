package resample

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/audio"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/framequeue"
)

type Engine struct {
	config config.ResampleConfig
	queue  framequeue.Single

	initialized   bool
	inputRate     int
	layout        media.ChannelLayout
	format        media.SampleFormat
	bits          int
	baseInputPTS  media.Pts
	baseOutputPTS media.Pts
	totalInput    int64
	nextNumerator int64
	emitted       int64
	last          []float32
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
	input, ok := (*frame).(*media.AudioFrame)
	if !ok {
		return fmt.Errorf("resample expected *media.AudioFrame, got %T", *frame)
	}
	if !e.initialized {
		e.initialize(block, input)
	} else if err := e.validateInput(block, input); err != nil {
		return err
	}
	if e.inputRate == e.config.SampleRate {
		input.Retain()
		return e.queue.Push(input)
	}

	output := audio.Block{
		Channels: make([][]float32, len(block.Channels)),
		Layout:   block.Layout,
		Rate:     e.config.SampleRate,
		PTS:      e.baseOutputPTS + media.Pts(e.emitted),
		Metadata: block.Metadata,
	}
	for sample := 0; sample < block.Samples(); sample++ {
		if e.totalInput == 0 {
			for channel := range block.Channels {
				e.last[channel] = block.Channels[channel][sample]
			}
			e.totalInput++
			continue
		}
		pairStart := e.totalInput - 1
		upper := (pairStart + 1) * int64(e.config.SampleRate)
		for e.nextNumerator < upper {
			fraction := float32(e.nextNumerator-pairStart*int64(e.config.SampleRate)) / float32(e.config.SampleRate)
			for channel := range output.Channels {
				next := block.Channels[channel][sample]
				value := e.last[channel] + (next-e.last[channel])*fraction
				output.Channels[channel] = append(output.Channels[channel], value)
			}
			e.nextNumerator += int64(e.inputRate)
			e.emitted++
		}
		for channel := range block.Channels {
			e.last[channel] = block.Channels[channel][sample]
		}
		e.totalInput++
	}
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
	if !e.initialized || e.inputRate == e.config.SampleRate {
		e.queue.Flush()
		return nil
	}
	desired := (e.totalInput*int64(e.config.SampleRate) + int64(e.inputRate)/2) / int64(e.inputRate)
	if e.emitted < desired {
		output := audio.Block{
			Channels: make([][]float32, len(e.last)),
			Layout:   e.layout,
			Rate:     e.config.SampleRate,
			PTS:      e.baseOutputPTS + media.Pts(e.emitted),
		}
		for e.emitted < desired {
			for channel := range output.Channels {
				output.Channels[channel] = append(output.Channels[channel], e.last[channel])
			}
			e.nextNumerator += int64(e.inputRate)
			e.emitted++
		}
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

func (e *Engine) initialize(block audio.Block, input *media.AudioFrame) {
	e.initialized = true
	e.inputRate = block.Rate
	e.layout = block.Layout
	e.format = input.Format
	e.bits = input.BitsPerSample
	e.baseInputPTS = block.PTS
	e.baseOutputPTS = rescalePTS(block.PTS, e.inputRate, e.config.SampleRate)
	e.last = make([]float32, len(block.Channels))
}

func (e *Engine) validateInput(block audio.Block, input *media.AudioFrame) error {
	if block.Rate != e.inputRate || block.Layout != e.layout || input.Format != e.format || input.BitsPerSample != e.bits {
		return fmt.Errorf("resample input format changed within stream")
	}
	if block.PTS != e.baseInputPTS+media.Pts(e.totalInput) {
		return fmt.Errorf("resample input PTS discontinuity: got %d, want %d", block.PTS, e.baseInputPTS+media.Pts(e.totalInput))
	}
	return nil
}

func rescalePTS(value media.Pts, fromRate, toRate int) media.Pts {
	if fromRate <= 0 || toRate <= 0 {
		return value
	}
	return media.Pts((int64(value)*int64(toRate) + int64(fromRate)/2) / int64(fromRate))
}
