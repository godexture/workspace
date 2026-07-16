package encoder

import "github.com/godexture/codec-flac/internal/flac"

type windowSet struct {
	funcs    []flac.Apodization
	byLength map[int][][]float64
}

func newWindowSet(funcs []flac.Apodization) windowSet {
	return windowSet{funcs: funcs, byLength: make(map[int][][]float64)}
}

func (windows *windowSet) forLength(length int) [][]float64 {
	if len(windows.funcs) == 0 {
		return nil
	}
	if cached, ok := windows.byLength[length]; ok {
		return cached
	}
	result := make([][]float64, 0, len(windows.funcs))
	for _, function := range windows.funcs {
		if function == nil {
			continue
		}
		window := make([]float64, length)
		function(window)
		result = append(result, window)
	}
	windows.byLength[length] = result
	return result
}
