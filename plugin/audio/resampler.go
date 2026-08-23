package audio

// resampler is streaming linear interpolation between two rates. It keeps the
// last input sample of every channel so a frame boundary falls in the middle
// of a pair like any other position, and counts in numerators rather than
// fractions so a long stream cannot drift.
//
// The rate it interpolates toward and the rate its output is labelled with are
// separate numbers. They are the same for a resample, where the stream really
// does change rate; a retime keeps the label and moves only the samples, which
// is what makes it play faster rather than sound the same for longer.
type resampler struct {
	inputRate, targetRate int
	last                  []float32
	started               bool
	totalInput            int64
	nextNumerator         int64
	emitted               int64
}

func newResampler(inputRate, targetRate, channels int) *resampler {
	return &resampler{inputRate: inputRate, targetRate: targetRate, last: make([]float32, channels)}
}

// capacity bounds what an input of this length can produce, so the operator
// can lease planes before any arithmetic runs. One pair of input samples can
// span several output positions, and rounding leaves room for one more.
func (r *resampler) capacity(samples int) int {
	return int(int64(samples)*int64(r.targetRate)/int64(r.inputRate)) + 2
}

func (r *resampler) produce(out, in [][]float32) int {
	written := 0
	for sample := range in[0] {
		if !r.started {
			for channel := range in {
				r.last[channel] = in[channel][sample]
			}
			r.started = true
			r.totalInput++
			continue
		}
		pairStart := r.totalInput - 1
		upper := (pairStart + 1) * int64(r.targetRate)
		for r.nextNumerator < upper {
			fraction := float32(r.nextNumerator-pairStart*int64(r.targetRate)) / float32(r.targetRate)
			for channel := range out {
				previous := r.last[channel]
				out[channel][written] = previous + (in[channel][sample]-previous)*fraction
			}
			written++
			r.nextNumerator += int64(r.inputRate)
			r.emitted++
		}
		for channel := range in {
			r.last[channel] = in[channel][sample]
		}
		r.totalInput++
	}
	return written
}

// pending is what the end of the stream still owes: rounding leaves the last
// input sample undelivered, and holding it is the difference between a stream
// that ends where it should and one a sample short.
func (r *resampler) pending() int {
	if !r.started {
		return 0
	}
	return int(max(r.desired()-r.emitted, 0))
}

func (r *resampler) desired() int64 {
	return (r.totalInput*int64(r.targetRate) + int64(r.inputRate)/2) / int64(r.inputRate)
}

func (r *resampler) drain(out [][]float32) int {
	written := 0
	for r.emitted < r.desired() {
		for channel := range out {
			out[channel][written] = r.last[channel]
		}
		written++
		r.nextNumerator += int64(r.inputRate)
		r.emitted++
	}
	return written
}

// rescale converts a timestamp counted in one rate into the same instant
// counted in another, rounding to the nearest position.
func rescale(value int64, from, to int) int64 {
	if from <= 0 || to <= 0 {
		return value
	}
	return (value*int64(to) + int64(from)/2) / int64(from)
}
