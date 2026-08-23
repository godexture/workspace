package audio

import (
	"math"
	"time"
)

// silenceFloorDB is where the level detectors stop looking down. Amplitudes
// below it are silence as far as a threshold is concerned, and the floor is
// what keeps a logarithm of zero out of the loop.
const silenceFloorDB = -120

func decibels(value float32) float64 {
	if value <= 0 {
		return silenceFloorDB
	}
	return max(20*math.Log10(float64(value)), silenceFloorDB)
}

func amplitude(value float64) float32 { return float32(math.Pow(10, value/20)) }

// timeConstant is the one-pole coefficient that reaches 1-1/e of a step in the
// given time at this rate. Zero means the detector follows its input at once.
func timeConstant(span time.Duration, rate int) float32 {
	if span <= 0 || rate <= 0 {
		return 0
	}
	return float32(math.Exp(-1 / (span.Seconds() * float64(rate))))
}

// linkedPeak is the loudest sample across every channel at one position.
// Detectors read it rather than each channel's own level so that a stereo
// signal is acted on identically on both sides; independent per-channel gain
// would move the image every time one side got louder.
func linkedPeak(planes [][]float32, index int) float32 {
	var result float32
	for _, samples := range planes {
		value := samples[index]
		if value < 0 {
			value = -value
		}
		if value > result {
			result = value
		}
	}
	return result
}

// peakOf is the loudest sample in one plane.
func peakOf(samples []float32) float32 {
	var result float32
	for _, value := range samples {
		if value < 0 {
			value = -value
		}
		if value > result {
			result = value
		}
	}
	return result
}
