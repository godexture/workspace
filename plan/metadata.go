package plan

import "github.com/godexture/godec/media/metadata/loss"

// PredictedMetadataLoss is one metadata encoding fact projected onto a job
// output before that output has committed.
type PredictedMetadataLoss struct {
	Output    int
	Node      string
	Component string
	Port      string
	Report    loss.Report
}

func (l PredictedMetadataLoss) Valid() bool {
	return l.Output >= 0 && l.Node != "" && l.Component != "" && l.Port != "" && l.Report.Valid()
}

func (l PredictedMetadataLoss) Lossy() bool { return l.Report.Lossy() }
