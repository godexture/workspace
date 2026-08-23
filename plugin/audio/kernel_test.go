package audio

import (
	"math"
	"math/rand/v2"
	"testing"
)

type signal struct {
	name   string
	planes [][]float32
}

// signals are the four shapes a stateful filter has to survive: one sample on
// its own, a sudden level change, a steady tone, and one with no structure at
// all. Each channel is offset so a filter that mixes its per-channel state
// shows it.
func signals(channels, samples int) []signal {
	generate := func(name string, at func(channel, index int) float32) signal {
		planes := make([][]float32, channels)
		for channel := range planes {
			planes[channel] = make([]float32, samples)
			for index := range planes[channel] {
				planes[channel][index] = at(channel, index)
			}
		}
		return signal{name: name, planes: planes}
	}
	source := rand.New(rand.NewPCG(1, 2))
	return []signal{
		generate("impulse", func(channel, index int) float32 {
			if index == channel {
				return 1
			}
			return 0
		}),
		generate("step", func(channel, index int) float32 {
			if index < samples/2 {
				return 0
			}
			return 0.5 + float32(channel)*0.1
		}),
		generate("sine", func(channel, index int) float32 {
			return float32(math.Sin(2 * math.Pi * float64(index+channel) / 16))
		}),
		generate("noise", func(int, int) float32 { return source.Float32()*2 - 1 }),
	}
}

// chunkInvariant is the property every stateful filter owes a stream: where
// the frame boundaries fall is the caller's business, not the signal's. A
// filter that resets between calls, or that keeps state per call rather than
// per stream, passes every single-frame test and fails this one.
func chunkInvariant(t *testing.T, build func() filter, channels, samples int) {
	t.Helper()
	for _, shape := range signals(channels, samples) {
		t.Run(shape.name, func(t *testing.T) {
			whole := clonePlanes(shape.planes)
			build().Apply(whole)

			split := clonePlanes(shape.planes)
			instance := build()
			for _, part := range chunks(split, 1, 3, samples-9, 5) {
				instance.Apply(part)
			}
			for channel := range whole {
				for index := range whole[channel] {
					if whole[channel][index] != split[channel][index] {
						t.Fatalf("channel %d sample %d: whole %v, split %v", channel, index,
							whole[channel][index], split[channel][index])
					}
				}
			}
		})
	}
}

func clonePlanes(planes [][]float32) [][]float32 {
	result := make([][]float32, len(planes))
	for index, plane := range planes {
		result[index] = append([]float32(nil), plane...)
	}
	return result
}

// chunks cuts planes into views that still write through to planes, so a
// filter driven chunk by chunk leaves its result where the caller can compare
// it with the one produced in a single call.
func chunks(planes [][]float32, sizes ...int) [][][]float32 {
	var result [][][]float32
	offset := 0
	for _, size := range sizes {
		if size <= 0 || offset >= len(planes[0]) {
			continue
		}
		end := min(offset+size, len(planes[0]))
		part := make([][]float32, len(planes))
		for channel, plane := range planes {
			part[channel] = plane[offset:end]
		}
		result = append(result, part)
		offset = end
	}
	return result
}
