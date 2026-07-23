package gate

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
	cfg          config.GateConfig
	linThreshold float32 // hard mode
	attackCoeff  float32 // lowpass mode
	releaseCoeff float32 // lowpass mode
	rateSet      bool
	rate         int
	envelope     float32 // lowpass mode: smoothed openness, 0 (closed) to 1 (open)
	state        []float32
	slot         buffer.Slot[media.Frame]
}

func New(cfg config.GateConfig) (*Engine, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	e := &Engine{cfg: cfg}
	if cfg.GateMode == config.GateModeHard {
		e.linThreshold = float32(math.Pow(10, cfg.ThresholdDBFS/20))
	}
	return e, nil
}

func (e *Engine) ensureRate(rate int) error {
	if e.rateSet {
		if e.rate != rate {
			return fmt.Errorf("gate input sample rate changed within stream")
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
		return fmt.Errorf("gate received nil frame")
	}
	input, ok := (*frame).(*media.AudioFrame)
	if !ok {
		return fmt.Errorf("gate expected *media.AudioFrame, got %T", *frame)
	}
	block, err := audio.Decode(frame)
	if err != nil {
		return err
	}
	if err := e.ensureRate(block.Rate); err != nil {
		return err
	}
	if e.cfg.GateMode == config.GateModeLowpass {
		e.processLowpass(block)
	} else {
		e.processHard(block)
	}
	output, err := audio.Encode(block, input.Format, input.BitsPerSample)
	if err != nil {
		return err
	}
	return e.slot.Push(output)
}

// processHard silences a sample index only when every channel is below the
// threshold there, so multi-channel audio never desyncs across channels.
func (e *Engine) processHard(block audio.Block) {
	for i := 0; i < block.Samples(); i++ {
		active := false
		for _, values := range block.Channels {
			if float32(math.Abs(float64(values[i]))) >= e.linThreshold {
				active = true
				break
			}
		}
		if !active {
			for _, values := range block.Channels {
				values[i] = 0
			}
		}
	}
}

// processLowpass implements a Buchla-style low-pass gate: a single envelope
// (linked across channels, like processHard's detector) simultaneously
// drives a one-pole low-pass filter's cutoff and the output level, so the
// sound darkens and fades out together as it falls below the threshold
// instead of being cut off abruptly.
func (e *Engine) processLowpass(block audio.Block) {
	if len(e.state) != len(block.Channels) {
		e.state = make([]float32, len(block.Channels))
	}
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
		target := opennessTarget(amplitudeToDB(peak), e.cfg.ThresholdDBFS, e.cfg.RangeDB)
		if target > e.envelope {
			e.envelope = e.attackCoeff*e.envelope + (1-e.attackCoeff)*target
		} else {
			e.envelope = e.releaseCoeff*e.envelope + (1-e.releaseCoeff)*target
		}
		cutoff := logInterp(e.cfg.CloseFrequencyHz, e.cfg.OpenFrequencyHz, e.envelope)
		coeff := onePoleCoeff(cutoff, e.rate)
		for channel, values := range block.Channels {
			filtered := e.state[channel] + coeff*(values[i]-e.state[channel])
			e.state[channel] = filtered
			values[i] = filtered * e.envelope
		}
	}
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

// opennessTarget maps a level to how open the gate should be: 1 at or above
// the threshold, 0 once it's rangeDB below the threshold, linear between.
func opennessTarget(levelDB, thresholdDB, rangeDB float64) float32 {
	if rangeDB <= 0 {
		if levelDB >= thresholdDB {
			return 1
		}
		return 0
	}
	t := (levelDB - (thresholdDB - rangeDB)) / rangeDB
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return float32(t)
}

// logInterp sweeps the cutoff exponentially (as pitch/frequency is
// perceived) between the closed and open corner frequencies.
func logInterp(minHz, maxHz float64, t float32) float64 {
	return minHz * math.Pow(maxHz/minHz, float64(t))
}

// onePoleCoeff is the standard one-pole low-pass coefficient for
// y[n] = y[n-1] + coeff*(x[n]-y[n-1]).
func onePoleCoeff(cutoffHz float64, rate int) float32 {
	return float32(1 - math.Exp(-2*math.Pi*cutoffHz/float64(rate)))
}
