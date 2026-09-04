// Package evidence contains stable projections shared by user-facing
// diagnostics. It deliberately keeps presentation details out of metadata's
// loss model.
package evidence

import "github.com/godexture/godec/media/metadata/loss"

// MetadataLoss returns the fixed report-detail schema used by planning,
// strict-policy, and committed-output diagnostics. Empty source and target
// values remain present so consumers can rely on the same fields for every
// valid report. A non-converted loss has no mapping, so its value is "none".
func MetadataLoss(report loss.Report) map[string]string {
	mapping := report.Loss.Mapping.String()
	if report.Loss.Kind != loss.Converted {
		mapping = "none"
	}
	source := report.Loss.Source
	return map[string]string{
		"block":          report.Block,
		"carrier":        report.Carrier.String(),
		"encoding":       report.Encoding,
		"key":            report.Loss.Key.String(),
		"kind":           report.Loss.Kind.String(),
		"mapping":        mapping,
		"native":         report.Loss.Native,
		"reason":         report.Loss.Detail,
		"sourceBlock":    source.Block,
		"sourceCarrier":  source.Carrier.String(),
		"sourceEncoding": source.Encoding,
		"sourceNative":   source.Native,
		"target":         report.Loss.Target.String(),
	}
}
