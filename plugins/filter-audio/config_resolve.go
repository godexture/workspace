package filter

import (
	"strconv"
	"strings"

	setting "github.com/godexture/sdk/config"

	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/equalizer"
)

func (c *EqualizerConfig) ResolveConfiguration(context setting.Context) ([]setting.Field, error) {
	field := setting.Field{
		Name: "gains", Active: c.EqualizerMode == EqualizerModeMultiband, Unit: "dB",
		DependsOn: []string{"mode", "bands", "low-hz", "high-hz", "manual-bands"},
		Range:     &setting.Range{Min: -12, Max: 12, Step: 0.5},
	}
	if !field.Active {
		return []setting.Field{field}, nil
	}

	axis, err := equalizer.ResolveAxis(c.Resolve())
	if err != nil {
		return nil, err
	}
	gains, err := config.ParseBandList(c.Gains)
	if err != nil {
		return nil, err
	}
	if !context.Explicit.Has("gains") || context.Mode == setting.Draft {
		resized := make([]float64, len(axis))
		copy(resized, gains)
		gains = resized
		c.Gains = formatList(gains)
	}
	field.Slots = make([]setting.Slot, len(axis))
	for index, band := range axis {
		field.Slots[index] = setting.Slot{
			Index: band.GainIndex, Label: formatFrequency(band.Frequency), Default: 0,
		}
	}
	return []setting.Field{field}, nil
}

func formatList(values []float64) string {
	items := make([]string, len(values))
	for index, value := range values {
		items[index] = strconv.FormatFloat(value, 'g', -1, 64)
	}
	return strings.Join(items, ",")
}

func formatFrequency(frequency float64) string {
	if frequency >= 1000 {
		value := strings.TrimSuffix(strconv.FormatFloat(frequency/1000, 'f', 1, 64), ".0")
		return value + " kHz"
	}
	precision := 1
	if frequency >= 100 {
		precision = 0
	}
	value := strings.TrimSuffix(strconv.FormatFloat(frequency, 'f', precision, 64), ".0")
	return value + " Hz"
}
