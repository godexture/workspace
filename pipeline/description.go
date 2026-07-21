package pipeline

import (
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/registry"
)

type NodeDescription struct {
	ID            string
	Role          manifest.NodeType
	Plugin        string
	Configuration registry.Configuration
	Resources     registry.ResourceBudget
	Inputs        []media.StreamInfo
	Outputs       []media.StreamInfo
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

func cloneDescription(description Description) Description {
	cloned := Description{
		Nodes: make([]NodeDescription, len(description.Nodes)),
		Edges: make([]EdgeDescription, len(description.Edges)),
	}
	for i, current := range description.Nodes {
		current.Inputs = cloneStreams(current.Inputs)
		current.Outputs = cloneStreams(current.Outputs)
		cloned.Nodes[i] = current
	}
	for i, current := range description.Edges {
		current.Stream = cloneStream(current.Stream)
		cloned.Edges[i] = current
	}
	return cloned
}

func cloneStreams(streams []media.StreamInfo) []media.StreamInfo {
	cloned := make([]media.StreamInfo, len(streams))
	for i := range streams {
		cloned[i] = cloneStream(streams[i])
	}
	return cloned
}

func cloneStream(stream media.StreamInfo) media.StreamInfo {
	stream.Metadata = *stream.Metadata.Clone()
	stream.CodecParameters = stream.CodecParameters.Clone()
	return stream
}
