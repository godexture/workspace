package host

import "github.com/godexture/godec/media/metadata/loss"

// ActualMetadataLoss is metadata loss evidence for an output that committed.
// Unlike plan.PredictedMetadataLoss, it is a statement about a completed run.
type ActualMetadataLoss struct {
	Output    int
	Node      string
	Component string
	Port      string
	Report    loss.Report
}

func (l ActualMetadataLoss) Valid() bool {
	return l.Output >= 0 && l.Node != "" && l.Component != "" && l.Port != "" && l.Report.Valid()
}

func (l ActualMetadataLoss) Lossy() bool { return l.Report.Lossy() }
