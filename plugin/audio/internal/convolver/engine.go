// Package convolver implements uniform partitioned FFT convolution: the
// impulse response is split into hop-sized partitions, each convolved in
// the frequency domain with the matching hop of recent input and summed
// via a frequency-domain delay line (FDL). Added latency equals one hop,
// independent of impulse response length, because the FDL lets partitions
// further back in the impulse response contribute to later hops without
// widening the transform used per hop.
package convolver

import (
	"fmt"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/buffer"
	"github.com/godexture/godec/sdk/dsp/fft"
)

const defaultBlockSize = 4096

// partition is one hop-sized segment of the impulse response, forward
// transformed once when the Engine is constructed.
type partition struct {
	spectrum []complex64
}

// channelState is the per-input-channel overlap-save window and
// frequency-domain delay line.
type channelState struct {
	history    []float32     // len hop: previous hop's raw input samples
	pending    []float32     // samples appended by SendFrame, not yet consumed into a hop
	window     []float32     // len 2*hop scratch: [history | newest hop]
	timeDomain []float32     // len 2*hop scratch: Inverse() output
	delayLine  [][]complex64 // [depth][bins]: spectra of the last depth hops, newest at head
	accum      []complex64   // len bins scratch: this hop's spectral sum
	head       int
}

// Engine performs streaming uniform partitioned convolution. See the
// package doc for the algorithm.
type Engine struct {
	cfg  config.ConvolutionConfig
	hop  int
	plan *fft.RealPlan
	bins int
	pool *registry.WorkerPool

	partitions [][]partition // [irChannel][partition]; len 1 when broadcasting to every input channel
	tailHops   int           // extra silent hops owed on Flush to drain the impulse response tail
	irRate     int
	irLayout   media.ChannelLayout
	irPTS      media.Pts
	irSamples  int64
	irFrames   int
	ir         [][]float32

	rateSet  bool
	rate     int
	format   media.SampleFormat
	bits     int
	layout   media.ChannelLayout
	channels []channelState

	basePTS     media.Pts
	totalInput  int64
	hopsEmitted int64

	queue   buffer.Queue[media.Frame]
	flushed bool

	// scratch is safe to share between the "in" and "ir" ports: node.Filter's
	// adapter (pkg/engine.FilterAdapter) pulls each input port on its own
	// goroutine, but always invokes SendFrame/SendInput/ReceiveFrame from a
	// single consumer goroutine, and Preload (which drains "ir") always
	// finishes before Start (which drains "in") begins -- so this Engine's
	// own methods are never called concurrently with each other.
	scratch audio.Scratch
}

func New(cfg config.ConvolutionConfig) (*Engine, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	hop := nextPowerOfTwo(cfg.BlockSize)
	if hop == 0 {
		hop = defaultBlockSize
	}
	return &Engine{
		cfg: cfg,
		hop: hop,
	}, nil
}

func (e *Engine) Prepare(resources registry.ResourceGrant) error {
	if e.plan != nil {
		return nil
	}
	plan, err := fft.NewRealPlan(2 * e.hop)
	if err != nil {
		return err
	}
	e.plan = plan
	e.bins = plan.Bins()
	e.pool = resources.Pool
	if len(e.cfg.ImpulseResponse) == 0 {
		return nil
	}
	return e.buildImpulse(e.cfg.ImpulseResponse, e.cfg.ImpulseRate)
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if e.plan == nil {
		return fmt.Errorf("convolver is not prepared")
	}
	if len(e.partitions) == 0 {
		return fmt.Errorf("convolver has no impulse response")
	}
	if e.flushed {
		return fmt.Errorf("convolver received a frame after flush")
	}
	block, err := audio.DecodeInto(frame, &e.scratch)
	if err != nil {
		return err
	}
	if err := e.ensureChannels(block); err != nil {
		return err
	}
	for ch, values := range block.Channels {
		e.channels[ch].pending = append(e.channels[ch].pending, values...)
	}
	e.totalInput += int64(block.Samples())
	return e.processHops()
}

func (e *Engine) SendInput(port string, frame *media.Frame) error {
	if port != "ir" {
		return fmt.Errorf("convolver has no auxiliary input port %q", port)
	}
	if e.plan == nil {
		return fmt.Errorf("convolver is not prepared")
	}
	if len(e.cfg.ImpulseResponse) != 0 {
		return fmt.Errorf("convolver impulse response is already configured")
	}
	block, err := audio.DecodeInto(frame, &e.scratch)
	if err != nil {
		return err
	}
	if e.irFrames == 0 {
		e.irRate = block.Rate
		e.irLayout = block.Layout
		e.irPTS = block.PTS
		e.ir = make([][]float32, len(block.Channels))
	} else if block.Rate != e.irRate || block.Layout != e.irLayout || block.PTS != e.irPTS+media.Pts(e.irSamples) {
		return fmt.Errorf("convolution impulse response format changed within stream")
	}
	if len(block.Channels) != len(e.ir) {
		return fmt.Errorf("convolution impulse response channel count changed within stream")
	}
	for channel, samples := range block.Channels {
		e.ir[channel] = append(e.ir[channel], samples...)
	}
	e.irSamples += int64(block.Samples())
	e.irFrames++
	return nil
}

func (e *Engine) EndInput(port string) error {
	switch port {
	case "in":
		return nil
	case "ir":
		if len(e.cfg.ImpulseResponse) != 0 {
			return fmt.Errorf("convolver impulse response is already configured")
		}
		if e.irFrames == 0 || e.irSamples == 0 {
			return fmt.Errorf("convolution impulse response input is empty")
		}
		if len(e.partitions) != 0 {
			return fmt.Errorf("convolution impulse response is already complete")
		}
		return e.buildImpulse(e.ir, e.irRate)
	default:
		return fmt.Errorf("convolver has no input port %q", port)
	}
}

func (e *Engine) ReceiveFrame() (media.Frame, error) {
	return e.queue.Receive()
}

func (e *Engine) Flush() error {
	if e.flushed {
		return nil
	}
	if e.rateSet {
		remainder := len(e.channels[0].pending) % e.hop
		pad := e.tailHops * e.hop
		if remainder != 0 {
			pad += e.hop - remainder
		}
		for ch := range e.channels {
			e.channels[ch].pending = append(e.channels[ch].pending, make([]float32, pad)...)
		}
		if err := e.processHops(); err != nil {
			return err
		}
	}
	e.flushed = true
	e.queue.Flush()
	return nil
}

func (e *Engine) Close() error {
	e.queue.Close()
	return nil
}

func nextPowerOfTwo(n int) int {
	if n <= 0 {
		return 0
	}
	power := 1
	for power < n {
		power <<= 1
	}
	return power
}
