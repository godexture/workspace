package normalize

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
	config  config.NormalizeConfig
	blocks  *spool.Blocks
	format  media.SampleFormat
	bits    int
	set     bool
	peak    float32
	factor  float32
	flushed bool
}

func New(config config.NormalizeConfig) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Engine{config: config, blocks: spool.New(config.MemoryLimitBytes, config.TempDir)}, nil
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if e.flushed {
		return fmt.Errorf("normalize received a frame after flush")
	}
	block, err := audio.Decode(frame)
	if err != nil {
		return err
	}
	input := (*frame).(*media.AudioFrame)
	if !e.set {
		e.format, e.bits, e.set = input.Format, input.BitsPerSample, true
	} else if e.format != input.Format || e.bits != input.BitsPerSample {
		return fmt.Errorf("normalize input format changed within stream")
	}
	for _, values := range block.Channels {
		for _, value := range values {
			e.peak = max(e.peak, float32(math.Abs(float64(value))))
		}
	}
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
	for _, values := range block.Channels {
		for i := range values {
			values[i] *= e.factor
		}
	}
	frame, err := audio.Encode(block, e.format, e.bits)
	if err != nil {
		return nil, err
	}
	var output media.Frame = frame
	return &output, nil
}

func (e *Engine) Flush() error {
	if e.flushed {
		return nil
	}
	target := float32(math.Pow(10, e.config.TargetPeakDBFS/20))
	e.factor = 1
	if e.peak > 0 {
		e.factor = target / e.peak
	}
	if !e.config.AllowAmplification && e.factor > 1 {
		e.factor = 1
	}
	e.flushed = true
	return e.blocks.Rewind()
}

func (e *Engine) Close() error { return e.blocks.Close() }
