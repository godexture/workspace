package compressor

import (
	"fmt"
	"math"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/sdk/audio"
	"github.com/godexture/sdk/buffer"
)

// silenceFloorDB keeps amplitudeToDB finite for zero/near-zero samples.
const silenceFloorDB = -120

type Engine struct {
	cfg          config.CompressorConfig
	makeupLinear float32
	attackCoeff  float32
	releaseCoeff float32
	rateSet      bool
	rate         int
	envelopeDB   float32
	slot         buffer.Slot[media.Frame]
}

func New(cfg config.CompressorConfig) (*Engine, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Engine{cfg: cfg, makeupLinear: float32(math.Pow(10, cfg.MakeupGainDB/20))}, nil
}

func (e *Engine) ensureRate(rate int) error {
	if e.rateSet {
		if e.rate != rate {
			return fmt.Errorf("compressor input sample rate changed within stream")
		}
		return nil
	}
	e.rate = rate
	e.attackCoeff = timeConstant(e.cfg.AttackMs, rate)
	e.releaseCoeff = timeConstant(e.cfg.ReleaseMs, rate)
	e.rateSet = true
	return nil
}

func timeConstant(ms float64, rate int) float32 {
	if ms <= 0 {
		return 0
	}
	return float32(math.Exp(-1 / (ms / 1000 * float64(rate))))
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if frame == nil || *frame == nil {
		return fmt.Errorf("compressor received nil frame")
	}
	input, ok := (*frame).(*media.AudioFrame)
	if !ok {
		return fmt.Errorf("compressor expected *media.AudioFrame, got %T", *frame)
	}
	block, err := audio.Decode(frame)
	if err != nil {
		return err
	}
	if err := e.ensureRate(block.Rate); err != nil {
		return err
	}
	// The detector links all channels (uses their peak, not per-channel levels)
	// so a stereo signal is gain-reduced identically on every channel; otherwise
	// independent per-channel gain would shift the stereo image.
	for i := 0; i < block.Samples(); i++ {
		var peak float32
		for _, values := range block.Channels {
			v := values[i]
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
		target := float32(gainReductionDB(amplitudeToDB(peak), e.cfg.ThresholdDBFS, e.cfg.Ratio, e.cfg.KneeDB))
		if target < e.envelopeDB {
			e.envelopeDB = e.attackCoeff*e.envelopeDB + (1-e.attackCoeff)*target
		} else {
			e.envelopeDB = e.releaseCoeff*e.envelopeDB + (1-e.releaseCoeff)*target
		}
		gain := float32(math.Pow(10, float64(e.envelopeDB)/20)) * e.makeupLinear
		for _, values := range block.Channels {
			values[i] *= gain
		}
	}
	output, err := audio.Encode(block, input.Format, input.BitsPerSample)
	if err != nil {
		return err
	}
	return e.slot.Push(output)
}

func (e *Engine) ReceiveFrame() (*media.Frame, error) {
	frame, err := e.slot.Receive()
	if err != nil {
		return nil, err
	}
	return &frame, nil
}
func (e *Engine) Flush() error { e.slot.Flush(); return nil }
func (e *Engine) Close() error { e.slot.Close(); return nil }

func amplitudeToDB(v float32) float64 {
	if v <= 0 {
		return silenceFloorDB
	}
	db := 20 * math.Log10(float64(v))
	if db < silenceFloorDB {
		return silenceFloorDB
	}
	return db
}

// gainReductionDB implements the standard feed-forward soft-knee compressor
// static curve (Giannoulis et al.), returning a value <= 0.
func gainReductionDB(levelDB, thresholdDB, ratio, kneeDB float64) float64 {
	overshoot := levelDB - thresholdDB
	half := kneeDB / 2
	switch {
	case kneeDB <= 0:
		if overshoot <= 0 {
			return 0
		}
		return overshoot * (1/ratio - 1)
	case overshoot <= -half:
		return 0
	case overshoot >= half:
		return overshoot * (1/ratio - 1)
	default:
		x := overshoot + half
		return (1/ratio - 1) * (x * x) / (2 * kneeDB)
	}
}
