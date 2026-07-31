package equalizer

import (
	"fmt"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/plugins/filter-audio/internal/config"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/buffer"
)

type channelState struct {
	x1, x2, y1, y2 float32
}

type Engine struct {
	cfg     config.EqualizerConfig
	bands   []bandSpec
	coeffs  []biquad
	state   [][]channelState
	rateSet bool
	rate    int
	slot    buffer.Slot[media.Frame]
	scratch audio.Scratch
}

func New(cfg config.EqualizerConfig) (*Engine, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	bands, err := resolveBands(cfg)
	if err != nil {
		return nil, err
	}
	return &Engine{cfg: cfg, bands: bands}, nil
}

func (e *Engine) ensureCoefficients(rate int) error {
	if e.rateSet {
		if e.rate != rate {
			return fmt.Errorf("equalizer input sample rate changed within stream")
		}
		return nil
	}
	e.coeffs = make([]biquad, len(e.bands))
	nyquistCap := float64(rate) / 2 * 0.999
	for index, band := range e.bands {
		frequency := band.freq
		if e.cfg.EqualizerMode == config.EqualizerModeMultiband && frequency > nyquistCap {
			frequency = nyquistCap
		}
		e.coeffs[index] = computeBiquad(band.eqType, frequency, band.gainDB, band.q, rate)
	}
	e.rate = rate
	e.rateSet = true
	return nil
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if frame == nil || *frame == nil {
		return fmt.Errorf("equalizer received nil frame")
	}
	input, ok := (*frame).(*media.AudioFrame)
	if !ok {
		return fmt.Errorf("equalizer expected *media.AudioFrame, got %T", *frame)
	}
	block, err := audio.DecodeInto(frame, &e.scratch)
	if err != nil {
		return err
	}
	if err := e.ensureCoefficients(block.Rate); err != nil {
		return err
	}
	if len(e.state) != len(e.bands) {
		e.state = make([][]channelState, len(e.bands))
	}
	for index := range e.bands {
		if len(e.state[index]) != len(block.Channels) {
			e.state[index] = make([]channelState, len(block.Channels))
		}
	}
	for band, coefficients := range e.coeffs {
		for channel, values := range block.Channels {
			state := &e.state[band][channel]
			for index, x0 := range values {
				y0 := coefficients.b0*x0 + coefficients.b1*state.x1 + coefficients.b2*state.x2 - coefficients.a1*state.y1 - coefficients.a2*state.y2
				state.x2, state.x1 = state.x1, x0
				state.y2, state.y1 = state.y1, y0
				values[index] = y0
			}
		}
	}
	output, err := audio.EncodeInto(block, input.Format, input.BitsPerSample, &e.scratch)
	if err != nil {
		return err
	}
	return e.slot.Push(output)
}

func (e *Engine) ReceiveFrame() (media.Frame, error) {
	return e.slot.Receive()
}
func (e *Engine) Flush() error { e.slot.Flush(); return nil }
func (e *Engine) Close() error { e.slot.Close(); return nil }
