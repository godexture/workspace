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
	"sync"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/sdk/audio"
	"github.com/godexture/sdk/buffer"
	"github.com/godexture/sdk/dsp"
	"github.com/godexture/sdk/dsp/fft"
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

func (e *Engine) buildImpulse(impulse [][]float32, rate int) error {
	ir := impulse
	if e.cfg.Normalize {
		ir = dsp.ClampL1(ir)
	}
	partitions := make([][]partition, len(ir))
	for ch, samples := range ir {
		parts, err := buildPartitions(e.plan, e.hop, samples, e.pool)
		if err != nil {
			return err
		}
		partitions[ch] = parts
	}
	e.partitions = partitions
	e.tailHops = len(partitions[0]) - 1
	e.irRate = rate
	return nil
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
	block, err := audio.Decode(frame)
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
	block, err := audio.Decode(frame)
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

func (e *Engine) ReceiveFrame() (*media.Frame, error) {
	frame, err := e.queue.Receive()
	if err != nil {
		return nil, err
	}
	return &frame, nil
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

func (e *Engine) ensureChannels(block audio.Block) error {
	if e.rateSet {
		return e.validateInput(block)
	}
	if e.irRate != 0 && e.irRate != block.Rate {
		return fmt.Errorf("convolution impulse response rate %d does not match input rate %d; resample the impulse response before use", e.irRate, block.Rate)
	}
	n := len(block.Channels)
	if len(e.partitions) != 1 && len(e.partitions) != n {
		return fmt.Errorf("convolution impulse response has %d channels, want 1 or %d", len(e.partitions), n)
	}

	e.rateSet = true
	e.rate = block.Rate
	e.format = block.Format
	e.bits = block.Bits
	e.layout = block.Layout
	e.basePTS = block.PTS

	e.channels = make([]channelState, n)
	for ch := range e.channels {
		depth := len(e.partitionsFor(ch))
		delayLine := make([][]complex64, depth)
		for i := range delayLine {
			delayLine[i] = make([]complex64, e.bins)
		}
		e.channels[ch] = channelState{
			history:    make([]float32, e.hop),
			window:     make([]float32, 2*e.hop),
			timeDomain: make([]float32, 2*e.hop),
			delayLine:  delayLine,
			accum:      make([]complex64, e.bins),
		}
	}
	return nil
}

func (e *Engine) validateInput(block audio.Block) error {
	if block.Rate != e.rate || block.Format != e.format || block.Bits != e.bits || block.Layout != e.layout {
		return fmt.Errorf("convolver input format changed within stream")
	}
	if len(block.Channels) != len(e.channels) {
		return fmt.Errorf("convolver input channel count changed within stream")
	}
	if block.PTS != e.basePTS+media.Pts(e.totalInput) {
		return fmt.Errorf("convolver input PTS discontinuity: got %d, want %d", block.PTS, e.basePTS+media.Pts(e.totalInput))
	}
	return nil
}

func (e *Engine) partitionsFor(channel int) []partition {
	if len(e.partitions) == 1 {
		return e.partitions[0]
	}
	return e.partitions[channel]
}

// processHops consumes complete hops from every channel's pending buffer
// (all channels always have equal pending length, since they are filled
// together from the same decoded blocks) until less than a hop remains.
func (e *Engine) processHops() error {
	for len(e.channels[0].pending) >= e.hop {
		output := make(audio.Channels, len(e.channels))
		for ch := range e.channels {
			out, err := e.processHop(ch)
			if err != nil {
				return err
			}
			output[ch] = out
		}
		if err := e.pushBlock(output); err != nil {
			return err
		}
	}
	return nil
}

// processHop consumes one hop's worth of pending samples on channel ch,
// advances its overlap-save window and frequency-domain delay line, and
// returns the hop's output samples (after wet/dry mixing).
func (e *Engine) processHop(ch int) ([]float32, error) {
	state := &e.channels[ch]
	newBlock := state.pending[:e.hop]

	copy(state.window[:e.hop], state.history)
	copy(state.window[e.hop:], newBlock)

	spectrum := state.delayLine[state.head]
	if err := e.plan.Forward(spectrum, state.window); err != nil {
		return nil, err
	}

	for k := range state.accum {
		state.accum[k] = 0
	}
	partitions := e.partitionsFor(ch)
	depth := len(state.delayLine)
	for p, part := range partitions {
		idx := state.head - p
		if idx < 0 {
			idx += depth
		}
		src := state.delayLine[idx]
		for k := range state.accum {
			state.accum[k] += src[k] * part.spectrum[k]
		}
	}

	if err := e.plan.Inverse(state.timeDomain, state.accum); err != nil {
		return nil, err
	}

	out := make([]float32, e.hop)
	copy(out, state.timeDomain[e.hop:])

	if mix := float32(e.cfg.WetDryMix); mix != 1 {
		dry := 1 - mix
		for i := range out {
			out[i] = newBlock[i]*dry + out[i]*mix
		}
	}

	copy(state.history, newBlock)
	state.head = (state.head + 1) % depth
	state.pending = state.pending[e.hop:]

	return out, nil
}

func (e *Engine) pushBlock(channels audio.Channels) error {
	block := audio.Block{
		Channels: channels,
		Layout:   e.layout,
		Rate:     e.rate,
		Format:   e.format,
		Bits:     e.bits,
		PTS:      e.basePTS + media.Pts(e.hopsEmitted)*media.Pts(e.hop),
	}
	e.hopsEmitted++
	frame, err := audio.Encode(block, e.format, e.bits)
	if err != nil {
		return err
	}
	return e.queue.Push(frame)
}

// partitionError collects the first error from concurrent partition tasks.
type partitionError struct {
	sync.Mutex
	value error
}

// partitionTask forward-transforms one partition on a shared WorkerPool
// worker. It implements registry.Task directly so it can be submitted
// without an extra closure allocation on top of the task struct itself.
type partitionTask struct {
	plan    *fft.RealPlan
	hop     int
	samples []float32
	index   int
	result  []partition
	group   *sync.WaitGroup
	errs    *partitionError
}

func (t *partitionTask) Run() {
	defer t.group.Done()
	part, err := transformPartition(t.plan.Clone(), t.hop, t.samples, t.index)
	if err != nil {
		t.errs.Lock()
		if t.errs.value == nil {
			t.errs.value = err
		}
		t.errs.Unlock()
		return
	}
	t.result[t.index] = part
}

// buildPartitions splits samples into hop-length segments (the last one
// zero-padded), and forward-transforms each into a spectrum ready for the
// frequency-domain delay line.
func buildPartitions(plan *fft.RealPlan, hop int, samples []float32, pool *registry.WorkerPool) ([]partition, error) {
	count := (len(samples) + hop - 1) / hop
	result := make([]partition, count)
	if pool == nil || count == 1 {
		for index := range result {
			part, err := transformPartition(plan, hop, samples, index)
			if err != nil {
				return nil, err
			}
			result[index] = part
		}
		return result, nil
	}

	var group sync.WaitGroup
	errs := &partitionError{}
	for i := range result {
		group.Add(1)
		pool.Submit(&partitionTask{
			plan:    plan,
			hop:     hop,
			samples: samples,
			index:   i,
			result:  result,
			group:   &group,
			errs:    errs,
		})
	}
	group.Wait()
	if errs.value != nil {
		return nil, errs.value
	}
	return result, nil
}

func transformPartition(plan *fft.RealPlan, hop int, samples []float32, index int) (partition, error) {
	windowed := make([]float32, 2*hop)
	start := index * hop
	end := min(start+hop, len(samples))
	copy(windowed[:hop], samples[start:end])
	spectrum := make([]complex64, plan.Bins())
	if err := plan.Forward(spectrum, windowed); err != nil {
		return partition{}, err
	}
	return partition{spectrum: spectrum}, nil
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
