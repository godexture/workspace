package linear

import (
	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/audio"
)

// Resampler implements streaming linear-interpolation resampling from
// inputRate to targetRate, labeling emitted blocks with outputRate. The
// distinction between targetRate and outputRate lets callers reuse the same
// interpolation math for both true sample-rate conversion (resample, where
// outputRate == targetRate) and playback-speed changes (speed, where
// outputRate stays pinned to inputRate while targetRate is virtual).
type Resampler struct {
	inputRate, targetRate, outputRate int
	layout                            media.ChannelLayout
	baseOutputPTS                     media.Pts

	totalInput    int64
	nextNumerator int64
	emitted       int64
	last          []float32
}

func NewResampler(inputRate, targetRate, outputRate int, basePTS media.Pts) *Resampler {
	return &Resampler{
		inputRate:     inputRate,
		targetRate:    targetRate,
		outputRate:    outputRate,
		baseOutputPTS: rescalePTS(basePTS, inputRate, targetRate),
	}
}

func (r *Resampler) Process(block audio.Block) audio.Block {
	if r.last == nil {
		r.layout = block.Layout
		r.last = make([]float32, len(block.Channels))
	}
	output := audio.Block{
		Channels: make([][]float32, len(block.Channels)),
		Layout:   block.Layout,
		Rate:     r.outputRate,
		PTS:      r.baseOutputPTS + media.Pts(r.emitted),
		Metadata: block.Metadata,
	}
	for sample := 0; sample < block.Samples(); sample++ {
		if r.totalInput == 0 {
			for channel := range block.Channels {
				r.last[channel] = block.Channels[channel][sample]
			}
			r.totalInput++
			continue
		}
		pairStart := r.totalInput - 1
		upper := (pairStart + 1) * int64(r.targetRate)
		for r.nextNumerator < upper {
			fraction := float32(r.nextNumerator-pairStart*int64(r.targetRate)) / float32(r.targetRate)
			for channel := range output.Channels {
				next := block.Channels[channel][sample]
				value := r.last[channel] + (next-r.last[channel])*fraction
				output.Channels[channel] = append(output.Channels[channel], value)
			}
			r.nextNumerator += int64(r.inputRate)
			r.emitted++
		}
		for channel := range block.Channels {
			r.last[channel] = block.Channels[channel][sample]
		}
		r.totalInput++
	}
	return output
}

// Finish emits the trailing tail block (holding the last input sample) that
// rounding leaves undelivered, if any. ok is false when nothing remains to
// flush.
func (r *Resampler) Finish() (audio.Block, bool) {
	desired := (r.totalInput*int64(r.targetRate) + int64(r.inputRate)/2) / int64(r.inputRate)
	if r.emitted >= desired {
		return audio.Block{}, false
	}
	output := audio.Block{
		Channels: make([][]float32, len(r.last)),
		Layout:   r.layout,
		Rate:     r.outputRate,
		PTS:      r.baseOutputPTS + media.Pts(r.emitted),
	}
	for r.emitted < desired {
		for channel := range output.Channels {
			output.Channels[channel] = append(output.Channels[channel], r.last[channel])
		}
		r.nextNumerator += int64(r.inputRate)
		r.emitted++
	}
	return output, true
}

func rescalePTS(value media.Pts, fromRate, toRate int) media.Pts {
	if fromRate <= 0 || toRate <= 0 {
		return value
	}
	return media.Pts((int64(value)*int64(toRate) + int64(fromRate)/2) / int64(fromRate))
}
