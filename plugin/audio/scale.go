package audio

import (
	"math"

	mediaaudio "github.com/godexture/godec/media/audio"
)

// fullScale reports the value a full-scale sample of S represents, and whether
// S is an integer representation that has to be scaled and clamped. Converting
// through a float64 pivot keeps every power-of-two integer conversion exact and
// needs one formula rather than one per pair of representations.
func fullScale[S mediaaudio.Sample]() (float64, bool) {
	switch any(*new(S)).(type) {
	case int16:
		return 1 << 15, true
	case int32:
		return 1 << 31, true
	default:
		return 1, false
	}
}

func widen[S mediaaudio.Sample](value S) float64 {
	scale, integral := fullScale[S]()
	if !integral {
		return float64(value)
	}
	return float64(value) / scale
}

// narrow returns the closest representable sample of S. A signal outside the
// nominal range saturates rather than wrapping, which is the difference
// between a clipped peak and a full-scale sign flip.
func narrow[S mediaaudio.Sample](value float64) S {
	scale, integral := fullScale[S]()
	if !integral {
		return S(value)
	}
	scaled := math.Round(value * scale)
	if scaled >= scale {
		scaled = scale - 1
	}
	if scaled < -scale {
		scaled = -scale
	}
	return S(scaled)
}

func convert[From, To mediaaudio.Sample](destination []To, source []From) {
	for index, value := range source {
		destination[index] = narrow[To](widen(value))
	}
}
