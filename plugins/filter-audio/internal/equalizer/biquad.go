package equalizer

import (
	"math"

	"github.com/godexture/filter-audio/internal/config"
)

type biquad struct {
	b0, b1, b2, a1, a2 float32
}

func computeBiquad(eqType config.EqualizerType, frequencyHz, gainDB, q float64, rate int) biquad {
	w0 := 2 * math.Pi * frequencyHz / float64(rate)
	cosW0, sinW0 := math.Cos(w0), math.Sin(w0)
	alpha := sinW0 / (2 * q)
	a := math.Pow(10, gainDB/40)

	var b0, b1, b2, a0, a1, a2 float64
	switch eqType {
	case config.EqualizerTypeLowShelf:
		sqrtA := math.Sqrt(a)
		b0 = a * ((a + 1) - (a-1)*cosW0 + 2*sqrtA*alpha)
		b1 = 2 * a * ((a - 1) - (a+1)*cosW0)
		b2 = a * ((a + 1) - (a-1)*cosW0 - 2*sqrtA*alpha)
		a0 = (a + 1) + (a-1)*cosW0 + 2*sqrtA*alpha
		a1 = -2 * ((a - 1) + (a+1)*cosW0)
		a2 = (a + 1) + (a-1)*cosW0 - 2*sqrtA*alpha
	case config.EqualizerTypeHighShelf:
		sqrtA := math.Sqrt(a)
		b0 = a * ((a + 1) + (a-1)*cosW0 + 2*sqrtA*alpha)
		b1 = -2 * a * ((a - 1) + (a+1)*cosW0)
		b2 = a * ((a + 1) + (a-1)*cosW0 - 2*sqrtA*alpha)
		a0 = (a + 1) - (a-1)*cosW0 + 2*sqrtA*alpha
		a1 = 2 * ((a - 1) - (a+1)*cosW0)
		a2 = (a + 1) - (a-1)*cosW0 - 2*sqrtA*alpha
	case config.EqualizerTypeLowPass:
		b0 = (1 - cosW0) / 2
		b1 = 1 - cosW0
		b2 = (1 - cosW0) / 2
		a0 = 1 + alpha
		a1 = -2 * cosW0
		a2 = 1 - alpha
	case config.EqualizerTypeHighPass:
		b0 = (1 + cosW0) / 2
		b1 = -(1 + cosW0)
		b2 = (1 + cosW0) / 2
		a0 = 1 + alpha
		a1 = -2 * cosW0
		a2 = 1 - alpha
	default:
		b0 = 1 + alpha*a
		b1 = -2 * cosW0
		b2 = 1 - alpha*a
		a0 = 1 + alpha/a
		a1 = -2 * cosW0
		a2 = 1 - alpha/a
	}
	return biquad{
		b0: float32(b0 / a0),
		b1: float32(b1 / a0),
		b2: float32(b2 / a0),
		a1: float32(a1 / a0),
		a2: float32(a2 / a0),
	}
}
