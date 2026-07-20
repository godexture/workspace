package fade

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/audio"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/spool"
	"github.com/godexture/sdk/engine"
)

type Engine struct {
	config       config.FadeConfig
	blocks       *spool.Blocks
	format       media.SampleFormat
	bits, rate   int
	total        int64
	set, flushed bool
}

func New(config config.FadeConfig) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Engine{config: config, blocks: spool.New(config.MemoryLimitBytes, config.TempDir)}, nil
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if e.flushed {
		return fmt.Errorf("fade received a frame after flush")
	}
	block, err := audio.Decode(frame)
	if err != nil {
		return err
	}
	if !e.set {
		e.format, e.bits, e.rate, e.set = block.Format, block.Bits, block.Rate, true
	} else if e.format != block.Format || e.bits != block.Bits || e.rate != block.Rate {
		return fmt.Errorf("fade input format changed within stream")
	}
	e.total += int64(block.Samples())
	return e.blocks.Append(block)
}

func (e *Engine) ReceiveFrame() (*media.Frame, error) {
	if !e.flushed {
		return nil, engine.ErrEAGAIN
	}
	block, ok, err := e.blocks.Next()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, engine.ErrEOF
	}
	in := int64(e.config.FadeIn) * int64(e.rate) / int64(1e9)
	out := int64(e.config.FadeOut) * int64(e.rate) / int64(1e9)
	start := int64(block.PTS) - int64(e.blocksStartPTS())
	for sample := 0; sample < block.Samples(); sample++ {
		position := start + int64(sample)
		gain := float32(1)
		if in > 0 && position < in {
			gain = float32(position) / float32(in)
		}
		remain := e.total - position
		if out > 0 && remain <= out {
			gain = min(gain, float32(max(remain, 0))/float32(out))
		}
		for _, values := range block.Channels {
			values[sample] *= gain
		}
	}
	frame, err := audio.Encode(block, e.format, e.bits)
	if err != nil {
		return nil, err
	}
	var output media.Frame = frame
	return &output, nil
}

func (e *Engine) blocksStartPTS() media.Pts { return e.blocks.FirstPTS() }

func (e *Engine) Flush() error {
	if e.flushed {
		return nil
	}
	e.flushed = true
	return e.blocks.Rewind()
}
func (e *Engine) Close() error { return e.blocks.Close() }
