package flac

import "math"

// Apodization fills window with analysis-window coefficients. Invalid windows
// only discard LPC candidates: residuals are always computed from raw samples.
type Apodization func(window []float64)

func Tukey(p float64) Apodization {
	p = clampUnit(p)
	return func(window []float64) { fillTukey(window, p) }
}

func SubdivideTukey(parts int, p float64) []Apodization {
	if parts < 1 {
		parts = 1
	}
	if parts > 7 {
		parts = 7
	}
	p = clampUnit(p)
	windows := make([]Apodization, 0, parts*(parts+1)/2)
	for divisions := 1; divisions <= parts; divisions++ {
		for part := 0; part < divisions; part++ {
			d, i := divisions, part
			windows = append(windows, func(window []float64) {
				start, end := i*len(window)/d, (i+1)*len(window)/d
				fillTukeySpan(window, start, end, p)
			})
		}
	}
	return windows
}

func clampUnit(value float64) float64 {
	if value < 0 || math.IsNaN(value) {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func fillTukey(window []float64, p float64) { fillTukeySpan(window, 0, len(window), p) }

func fillTukeySpan(window []float64, start, end int, p float64) {
	clear(window)
	length := end - start
	if length <= 0 {
		return
	}
	if length == 1 || p <= 0 {
		for i := start; i < end; i++ {
			window[i] = 1
		}
		return
	}
	taper := int(p/2*float64(length)) - 1
	if taper < 1 {
		for i := start; i < end; i++ {
			window[i] = 1
		}
		return
	}
	for i := 0; i <= taper; i++ {
		window[start+i] = 0.5 - 0.5*math.Cos(math.Pi*float64(i)/float64(taper))
		window[end-taper-1+i] = 0.5 - 0.5*math.Cos(math.Pi*float64(i+taper)/float64(taper))
	}
	for i := start + taper + 1; i < end-taper-1; i++ {
		window[i] = 1
	}
}
