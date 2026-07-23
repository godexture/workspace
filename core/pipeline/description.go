package pipeline

import (
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/registry"
)

type NodeDescription struct {
	ID     string
	Role   manifest.NodeType
	Plugin string
	// Configuration is excluded from JSON: plugin config types are free to
	// hold non-marshalable values (e.g. codec-flac's Apodizations, which are
	// func([]float64)), so this live object was never meant as a wire
	// format. Use cliflag.DescribeStruct for a safe, field-by-field view.
	Configuration registry.Configuration `json:"-"`
	Resources     registry.ResourceBudget
	Inputs        []media.StreamInfo
	Outputs       []media.StreamInfo
	AutoInserted  bool
}

type EdgeDescription struct {
	FromNode       string
	FromPort       string
	ToNode         string
	ToPort         string
	Stream         media.StreamInfo
	ProgressSource bool
}

type Description struct {
	Nodes []NodeDescription
	Edges []EdgeDescription
}

// Clone returns an independent copy that shares no state with d.
func (d Description) Clone() Description {
	cloned := Description{
		Nodes: make([]NodeDescription, len(d.Nodes)),
		Edges: make([]EdgeDescription, len(d.Edges)),
	}
	for i, current := range d.Nodes {
		current.Inputs = media.CloneStreams(current.Inputs)
		current.Outputs = media.CloneStreams(current.Outputs)
		cloned.Nodes[i] = current
	}
	for i, current := range d.Edges {
		current.Stream = current.Stream.Clone()
		cloned.Edges[i] = current
	}
	return cloned
}
