package audio

import (
	"github.com/godexture/godec/sdk/dsp"
	"github.com/godexture/godec/sdk/dsp/fft"
)

// Uniform partitioned convolution: the impulse response is cut into hop-sized
// partitions, each transformed once, and every hop of input is convolved with
// all of them through a frequency-domain delay line. What that buys is latency
// of one hop however long the response is, because a partition further back
// contributes to a later hop rather than widening the transform.
type partition struct{ spectrum []complex64 }

// channelConvolver is one input channel's overlap-save window and its delay
// line of recent spectra.
type channelConvolver struct {
	history     []float32
	window      []float32
	timeDomain  []float32
	delayLine   [][]complex64
	accumulator []complex64
	head        int
}

type convolver struct {
	plan *fft.RealPlan
	hop  int
	bins int
	// partitions holds one set per impulse response channel, or exactly one
	// set applied to every input channel when the response is mono.
	partitions [][]partition
	channels   []channelConvolver
	mix        float32
	// tail is how many silent hops the response still owes once the input has
	// ended, which is what stops a reverberation being cut off at the end.
	tail int
}

func newConvolver(impulse [][]float32, hop int, mix float32, normalize bool) (*convolver, error) {
	plan, err := fft.NewRealPlan(2 * hop)
	if err != nil {
		return nil, err
	}
	response := impulse
	if normalize {
		response = dsp.ClampL1(impulse)
	}
	result := &convolver{plan: plan, hop: hop, bins: plan.Bins(), mix: mix}
	result.partitions = make([][]partition, len(response))
	for channel, samples := range response {
		parts, partErr := partitionResponse(plan, hop, samples)
		if partErr != nil {
			return nil, partErr
		}
		result.partitions[channel] = parts
	}
	result.tail = len(result.partitions[0]) - 1
	return result, nil
}

// prepare gives every input channel the state its own partitions need. The
// channel count is not known until the signal arrives, which is later than the
// response was built.
func (c *convolver) prepare(channels int) {
	c.channels = make([]channelConvolver, channels)
	for index := range c.channels {
		depth := len(c.responseFor(index))
		delayLine := make([][]complex64, depth)
		for position := range delayLine {
			delayLine[position] = make([]complex64, c.bins)
		}
		c.channels[index] = channelConvolver{
			history:     make([]float32, c.hop),
			window:      make([]float32, 2*c.hop),
			timeDomain:  make([]float32, 2*c.hop),
			delayLine:   delayLine,
			accumulator: make([]complex64, c.bins),
		}
	}
}

// responseFor is the partition set one input channel is convolved with. A
// single-channel response applies to every input channel; otherwise each
// channel has its own and they are convolved independently, which is what
// cross-channel convolution is not.
func (c *convolver) responseFor(channel int) []partition {
	if len(c.partitions) == 1 {
		return c.partitions[0]
	}
	return c.partitions[channel]
}

func (c *convolver) channelCount() int { return len(c.partitions) }

// hopOf convolves one hop of one channel in place, reading from and writing to
// samples, which the caller has already sized at exactly one hop.
func (c *convolver) hopOf(channel int, samples []float32) error {
	state := &c.channels[channel]
	copy(state.window[:c.hop], state.history)
	copy(state.window[c.hop:], samples)
	copy(state.history, samples)

	spectrum := state.delayLine[state.head]
	if err := c.plan.Forward(spectrum, state.window); err != nil {
		return err
	}
	clear(state.accumulator)
	depth := len(state.delayLine)
	for offset, part := range c.responseFor(channel) {
		index := state.head - offset
		if index < 0 {
			index += depth
		}
		source := state.delayLine[index]
		for bin := range state.accumulator {
			state.accumulator[bin] += source[bin] * part.spectrum[bin]
		}
	}
	if err := c.plan.Inverse(state.timeDomain, state.accumulator); err != nil {
		return err
	}
	// Overlap-save keeps the second half, which is the part free of the
	// wrap-around the transform folds into the first.
	wet := state.timeDomain[c.hop:]
	if c.mix == 1 {
		copy(samples, wet)
	} else {
		dry := 1 - c.mix
		for index := range samples {
			samples[index] = samples[index]*dry + wet[index]*c.mix
		}
	}
	state.head = (state.head + 1) % depth
	return nil
}

func partitionResponse(plan *fft.RealPlan, hop int, samples []float32) ([]partition, error) {
	count := (len(samples) + hop - 1) / hop
	result := make([]partition, count)
	for index := range result {
		windowed := make([]float32, 2*hop)
		start := index * hop
		copy(windowed[:hop], samples[start:min(start+hop, len(samples))])
		spectrum := make([]complex64, plan.Bins())
		if err := plan.Forward(spectrum, windowed); err != nil {
			return nil, err
		}
		result[index] = partition{spectrum: spectrum}
	}
	return result, nil
}

func nextPowerOfTwo(value int) int {
	result := 1
	for result < value {
		result <<= 1
	}
	return result
}
