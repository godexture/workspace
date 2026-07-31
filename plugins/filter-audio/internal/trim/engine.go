package trim

import (
	"fmt"
	"math"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/plugins/filter-audio/internal/config"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/engine"
)

// tailBuffer retains blocks that might need to be replayed if a later block
// turns out to contain more activity. audio.Spool implements it exactly;
// silenceTail implements it approximately, by shape only.
type tailBuffer interface {
	Append(block audio.Block) error
	Rewind() error
	Next() (audio.Block, bool, error)
	Close() error
}

type Engine struct {
	config                        config.TrimConfig
	tail                          tailBuffer
	nextTail                      tailBuffer
	format                        media.SampleFormat
	bits                          int
	layout                        media.ChannelLayout
	rate                          int
	set, started, flushed, replay bool
	pending                       *audio.Block
	threshold                     float32
	trimLeading, trimTrailing     bool
	scratch                       audio.Scratch
}

func New(config config.TrimConfig) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	trimLeading, trimTrailing := trimSides(config.TrimMode)
	return &Engine{
		config:       config,
		tail:         newTailBuffer(config),
		threshold:    float32(math.Pow(10, config.ThresholdDBFS/20)),
		trimLeading:  trimLeading,
		trimTrailing: trimTrailing,
		started:      !trimLeading,
	}, nil
}

func newTailBuffer(cfg config.TrimConfig) tailBuffer {
	if cfg.ApproximateSilence {
		return &silenceTail{}
	}
	return audio.NewSpool(cfg.MemoryLimitBytes, cfg.TempDir)
}

// trimSides reports which ends of the stream mode trims silence from.
func trimSides(mode config.TrimMode) (leading, trailing bool) {
	return mode == config.TrimModeBoth || mode == config.TrimModeStart, mode == config.TrimModeBoth || mode == config.TrimModeEnd
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if e.flushed {
		return fmt.Errorf("trim received a frame after flush")
	}
	if e.replay || e.pending != nil {
		return fmt.Errorf("trim has unconsumed output")
	}
	block, err := audio.DecodeInto(frame, &e.scratch)
	if err != nil {
		return err
	}
	if !e.set {
		e.format, e.bits, e.layout, e.rate, e.set = block.Format, block.Bits, block.Layout, block.Rate, true
	} else if e.format != block.Format || e.bits != block.Bits || e.layout != block.Layout || e.rate != block.Rate {
		return fmt.Errorf("trim input format changed within stream")
	}

	if !e.started {
		first, last := activity(block, e.threshold)
		if last < 0 {
			return nil
		}
		e.started = true
		if !e.trimTrailing {
			e.pending = ptr(block.Slice(first, block.Samples()))
			return nil
		}
		e.pending = ptr(block.Slice(first, last+1))
		if last+1 < block.Samples() {
			return e.tail.Append(block.Slice(last+1, block.Samples()))
		}
		return nil
	}

	if !e.trimTrailing {
		e.pending = ptr(block)
		return nil
	}

	_, last := activity(block, e.threshold)
	if last < 0 {
		return e.tail.Append(block)
	}
	if err := e.tail.Rewind(); err != nil {
		return err
	}
	e.replay = true
	e.pending = ptr(block.Slice(0, last+1))
	e.nextTail = newTailBuffer(e.config)
	if last+1 < block.Samples() {
		return e.nextTail.Append(block.Slice(last+1, block.Samples()))
	}
	return nil
}

func (e *Engine) ReceiveFrame() (media.Frame, error) {
	if e.replay {
		block, ok, err := e.tail.Next()
		if err != nil {
			return nil, err
		}
		if ok {
			return e.encode(block)
		}
		if err := e.tail.Close(); err != nil {
			return nil, err
		}
		e.tail, e.nextTail, e.replay = e.nextTail, nil, false
		if e.tail == nil {
			e.tail = newTailBuffer(e.config)
		}
	}
	if e.pending != nil {
		block := *e.pending
		e.pending = nil
		return e.encode(block)
	}
	if e.flushed {
		return nil, engine.ErrEOF
	}
	return nil, engine.ErrEAGAIN
}

func (e *Engine) encode(block audio.Block) (media.Frame, error) {
	frame, err := audio.EncodeInto(block, e.format, e.bits, &e.scratch)
	if err != nil {
		return nil, err
	}
	return frame, nil
}
func (e *Engine) Flush() error { e.flushed = true; return nil }
func (e *Engine) Close() error {
	err := e.tail.Close()
	if e.nextTail != nil {
		if nextErr := e.nextTail.Close(); err == nil {
			err = nextErr
		}
	}
	return err
}

func activity(block audio.Block, threshold float32) (int, int) {
	first, last := -1, -1
	for sample := 0; sample < block.Samples(); sample++ {
		for _, values := range block.Channels {
			if float32(math.Abs(float64(values[sample]))) >= threshold {
				if first < 0 {
					first = sample
				}
				last = sample
				break
			}
		}
	}
	return first, last
}

func ptr(block audio.Block) *audio.Block { return &block }
