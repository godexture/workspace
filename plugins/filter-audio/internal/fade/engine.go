package fade

import (
	"fmt"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/plugins/filter-audio/internal/config"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/engine"
)

type Engine struct {
	config       config.FadeConfig
	blocks       *audio.Spool
	format       media.SampleFormat
	bits, rate   int
	total        int64
	set, flushed bool
	scratch      audio.Scratch
}

func New(config config.FadeConfig) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Engine{config: config, blocks: audio.NewSpool(config.MemoryLimitBytes, config.TempDir)}, nil
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if e.flushed {
		return fmt.Errorf("fade received a frame after flush")
	}
	block, err := audio.DecodeInto(frame, &e.scratch)
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

func (e *Engine) ReceiveFrame() (media.Frame, error) {
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
	frame, err := audio.EncodeInto(block, e.format, e.bits, &e.scratch)
	if err != nil {
		return nil, err
	}
	return frame, nil
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
