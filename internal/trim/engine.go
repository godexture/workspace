package trim

import (
	"fmt"
	"math"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/audio"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/spool"
	"github.com/godexture/sdk/engine"
)

type Engine struct {
	config                        config.TrimConfig
	tail                          *spool.Blocks
	nextTail                      *spool.Blocks
	format                        media.SampleFormat
	bits                          int
	layout                        media.ChannelLayout
	rate                          int
	set, started, flushed, replay bool
	pending                       *audio.Block
	threshold                     float32
}

func New(config config.TrimConfig) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Engine{config: config, tail: spool.New(config.MemoryLimitBytes, config.TempDir), threshold: float32(math.Pow(10, config.ThresholdDBFS/20))}, nil
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if e.flushed {
		return fmt.Errorf("trim received a frame after flush")
	}
	if e.replay || e.pending != nil {
		return fmt.Errorf("trim has unconsumed output")
	}
	block, err := audio.Decode(frame)
	if err != nil {
		return err
	}
	if !e.set {
		e.format, e.bits, e.layout, e.rate, e.set = block.Format, block.Bits, block.Layout, block.Rate, true
	} else if e.format != block.Format || e.bits != block.Bits || e.layout != block.Layout || e.rate != block.Rate {
		return fmt.Errorf("trim input format changed within stream")
	}
	first, last := activity(block, e.threshold)
	if !e.started {
		if last < 0 {
			return nil
		}
		e.started = true
		e.pending = ptr(block.Slice(first, last+1))
		if last+1 < block.Samples() {
			return e.tail.Append(block.Slice(last+1, block.Samples()))
		}
		return nil
	}
	if last < 0 {
		return e.tail.Append(block)
	}
	if err := e.tail.Rewind(); err != nil {
		return err
	}
	e.replay = true
	e.pending = ptr(block.Slice(0, last+1))
	e.nextTail = spool.New(e.config.MemoryLimitBytes, e.config.TempDir)
	if last+1 < block.Samples() {
		return e.nextTail.Append(block.Slice(last+1, block.Samples()))
	}
	return nil
}

func (e *Engine) ReceiveFrame() (*media.Frame, error) {
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
			e.tail = spool.New(e.config.MemoryLimitBytes, e.config.TempDir)
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

func (e *Engine) encode(block audio.Block) (*media.Frame, error) {
	frame, err := audio.Encode(block, e.format, e.bits)
	if err != nil {
		return nil, err
	}
	var output media.Frame = frame
	return &output, nil
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
