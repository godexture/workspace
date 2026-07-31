package encoder

import (
	"math"

	"github.com/godexture/godec/plugins/codec-flac/internal/config"
)

func lpcPrecisionCandidates(options config.EncoderConfig) []int {
	precision := options.LPCPrecision
	if precision == 0 {
		precision = config.DefaultLPCPrecision
	}
	if !options.EnablePrecisionSearch {
		return []int{precision}
	}
	low := min(5, precision)
	result := make([]int, 0, 16-low)
	for value := low; value <= 15; value++ {
		result = append(result, value)
	}
	return result
}

type lpcWorkspace struct {
	values          []float64
	auto            []float64
	sets            [][]float64
	setCoefficients []float64
	estimates       []float64
	coeff           []float64
}

func lpcCoefficientSets(samples []int64, maxOrder, precision int, mode config.OrderSearchMode, window []float64, bitsPerSample int, workspace *lpcWorkspace) [][]float64 {
	exhaustive := mode == config.OrderSearchExhaustive
	if maxOrder >= len(samples) {
		maxOrder = len(samples) - 1
	}
	if maxOrder <= 0 {
		return nil
	}
	if precision == 0 {
		precision = config.DefaultLPCPrecision
	}
	if window != nil && len(window) != len(samples) {
		return nil
	}
	// Levinson-Durbin recursion; see standard linear-prediction texts.
	workspace.values = resize(workspace.values, len(samples))
	values := workspace.values
	windowSamples(samples, window, values, bitsPerSample)
	workspace.auto = resize(workspace.auto, maxOrder+1)
	auto := workspace.auto
	autocorrelate(values, auto)
	if auto[0] == 0 {
		return nil
	}
	workspace.sets = resize(workspace.sets, maxOrder+1)
	clear(workspace.sets)
	sets := workspace.sets
	workspace.setCoefficients = resize(workspace.setCoefficients, maxOrder*(maxOrder+1)/2)
	workspace.estimates = resize(workspace.estimates, maxOrder+1)
	clear(workspace.estimates)
	estimates := workspace.estimates
	workspace.coeff = resize(workspace.coeff, maxOrder)
	coeff := workspace.coeff
	errorValue := auto[0]
	for i := 0; i < maxOrder; i++ {
		reflection := auto[i+1]
		for j := 0; j < i; j++ {
			reflection -= coeff[j] * auto[i-j]
		}
		if errorValue <= 0 {
			break
		}
		reflection /= errorValue
		if reflection <= -0.999999 || reflection >= 0.999999 || math.IsNaN(reflection) {
			break
		}
		for j := 0; j < i/2; j++ {
			front, back := coeff[j], coeff[i-1-j]
			coeff[j] = front - reflection*back
			coeff[i-1-j] = back - reflection*front
		}
		if i%2 == 1 {
			coeff[i/2] -= reflection * coeff[i/2]
		}
		coeff[i] = reflection
		errorValue *= 1 - reflection*reflection
		order := i + 1
		offset := order * (order - 1) / 2
		sets[order] = workspace.setCoefficients[offset : offset+order]
		copy(sets[order], coeff[:order])
		if exhaustive {
			continue
		}
		residualSamples := len(samples) - order
		estimates[order] = float64(residualSamples)*math.Max(0, 0.5*math.Log2(errorValue/float64(residualSamples))) + float64(8+order*precision+4+5)
	}
	if exhaustive {
		return sets
	}
	best := 0
	for order := 1; order <= maxOrder; order++ {
		if estimates[order] == 0 {
			continue
		}
		if best == 0 || estimates[order] < estimates[best] {
			best = order
		}
	}
	if best == 0 {
		return sets
	}
	for order := range sets {
		if order != best {
			sets[order] = nil
		}
	}
	return sets
}

func quantizeLPCCoefficients(coefficients []float64, precision int) ([]int64, int, bool) {
	var maxCoeff float64
	for _, coefficient := range coefficients {
		if magnitude := math.Abs(coefficient); magnitude > maxCoeff {
			maxCoeff = magnitude
		}
	}
	if maxCoeff <= 0 || math.IsInf(maxCoeff, 0) || math.IsNaN(maxCoeff) {
		return nil, 0, false
	}
	_, exponent := math.Frexp(maxCoeff)
	shift := precision - 1 - exponent
	if shift > 15 {
		shift = 15
	}
	if shift < 0 {
		shift = 0
	}
	minValue := -(int64(1) << uint(precision-1))
	maxValue := (int64(1) << uint(precision-1)) - 1
	quantized := make([]int64, len(coefficients))
	scale := math.Ldexp(1, shift)
	carry := 0.0
	for i, coefficient := range coefficients {
		value := coefficient*scale + carry
		rounded := math.Round(value)
		if rounded > float64(maxValue) {
			rounded = float64(maxValue)
		} else if rounded < float64(minValue) {
			rounded = float64(minValue)
		}
		carry = value - rounded
		quantized[i] = int64(rounded)
	}
	return quantized, shift, true
}
