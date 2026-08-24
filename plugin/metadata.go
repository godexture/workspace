package plugin

import "github.com/godexture/godec/media/metadata/loss"

// MetadataReport binds metadata loss evidence to the component output whose
// bytes carry it. Output must name a declared output port of the component.
type MetadataReport struct {
	Output string
	Report loss.Report
}

func (r MetadataReport) valid() bool { return r.Output != "" && r.Report.Valid() }
