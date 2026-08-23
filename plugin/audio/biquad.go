package audio

import "math"

// biquad holds the normalised coefficients of one second-order section, and
// biquadState the two inputs and two outputs it remembers. They are separate
// because every channel shares the coefficients and none shares the memory.
type biquad struct{ b0, b1, b2, a1, a2 float32 }

type biquadState struct{ x1, x2, y1, y2 float32 }

// newBiquad is the Audio EQ Cookbook (Bristow-Johnson) design for one section.
func newBiquad(kind bandType, frequency, gainDB, q float64, rate int) biquad {
	angle := 2 * math.Pi * frequency / float64(rate)
	cosine, sine := math.Cos(angle), math.Sin(angle)
	alpha := sine / (2 * q)
	gain := math.Pow(10, gainDB/40)

	var b0, b1, b2, a0, a1, a2 float64
	switch kind {
	case lowShelfBand:
		root := math.Sqrt(gain)
		b0 = gain * ((gain + 1) - (gain-1)*cosine + 2*root*alpha)
		b1 = 2 * gain * ((gain - 1) - (gain+1)*cosine)
		b2 = gain * ((gain + 1) - (gain-1)*cosine - 2*root*alpha)
		a0 = (gain + 1) + (gain-1)*cosine + 2*root*alpha
		a1 = -2 * ((gain - 1) + (gain+1)*cosine)
		a2 = (gain + 1) + (gain-1)*cosine - 2*root*alpha
	case highShelfBand:
		root := math.Sqrt(gain)
		b0 = gain * ((gain + 1) + (gain-1)*cosine + 2*root*alpha)
		b1 = -2 * gain * ((gain - 1) + (gain+1)*cosine)
		b2 = gain * ((gain + 1) + (gain-1)*cosine - 2*root*alpha)
		a0 = (gain + 1) - (gain-1)*cosine + 2*root*alpha
		a1 = 2 * ((gain - 1) - (gain+1)*cosine)
		a2 = (gain + 1) - (gain-1)*cosine - 2*root*alpha
	case lowPassBand:
		b0 = (1 - cosine) / 2
		b1 = 1 - cosine
		b2 = (1 - cosine) / 2
		a0 = 1 + alpha
		a1 = -2 * cosine
		a2 = 1 - alpha
	case highPassBand:
		b0 = (1 + cosine) / 2
		b1 = -(1 + cosine)
		b2 = (1 + cosine) / 2
		a0 = 1 + alpha
		a1 = -2 * cosine
		a2 = 1 - alpha
	default:
		b0 = 1 + alpha*gain
		b1 = -2 * cosine
		b2 = 1 - alpha*gain
		a0 = 1 + alpha/gain
		a1 = -2 * cosine
		a2 = 1 - alpha/gain
	}
	return biquad{
		b0: float32(b0 / a0),
		b1: float32(b1 / a0),
		b2: float32(b2 / a0),
		a1: float32(a1 / a0),
		a2: float32(a2 / a0),
	}
}
