package pipeline

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

func Link[T any, A node.OutputNode[T], B node.InputNode[T]](nodeA A, portA string, nodeB B, portB string) error {
	return LinkWithBufferSize(nodeA, portA, nodeB, portB, 100)
}

// LinkWithBufferSize connects two nodes using a channel edge with the requested
// capacity. A small capacity is useful for pipelines that must apply strict
// backpressure to large packets or frames.
func LinkWithBufferSize[T any, A node.OutputNode[T], B node.InputNode[T]](nodeA A, portA string, nodeB B, portB string, bufferSize int) error {
	if bufferSize < 0 {
		return fmt.Errorf("invalid edge buffer size: %d", bufferSize)
	}

	outPort, okA := nodeA.OutputPorts()[portA]
	if !okA {
		return fmt.Errorf("output port '%s' not found on node A", portA)
	}

	inPort, okB := nodeB.InputPorts()[portB]
	if !okB {
		return fmt.Errorf("input port '%s' not found on node B", portB)
	}

	edge := NewChanEdge[T](bufferSize)

	outPort.Connect(edge)
	inPort.Connect(edge)

	return nil
}

func LinkAny(nodeA node.Node, portA string, nodeB node.Node, portB string) error {
	if outA, okA := nodeA.(node.OutputNode[*media.Packet]); okA {
		if inB, okB := nodeB.(node.InputNode[*media.Packet]); okB {
			return Link(outA, portA, inB, portB)
		}
	}
	if outA, okA := nodeA.(node.OutputNode[media.Frame]); okA {
		if inB, okB := nodeB.(node.InputNode[media.Frame]); okB {
			return Link(outA, portA, inB, portB)
		}
	}
	return fmt.Errorf("incompatible nodes or port types: %T and %T", nodeA, nodeB)
}

type Builder struct{}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) Build(geo *Geometry) ([]node.Node, error) {
	nodeMap := make(map[string]node.Node)
	var nodeList []node.Node

	for _, n := range geo.Nodes {
		nodeMap[n.ID] = n.Node
		nodeList = append(nodeList, n.Node)
	}

	for _, e := range geo.Edges {
		fromNode, ok := nodeMap[e.FromNode]
		if !ok {
			return nil, fmt.Errorf("node not found: %s", e.FromNode)
		}
		toNode, ok := nodeMap[e.ToNode]
		if !ok {
			return nil, fmt.Errorf("node not found: %s", e.ToNode)
		}

		if err := LinkAny(fromNode, e.FromPort, toNode, e.ToPort); err != nil {
			return nil, fmt.Errorf("failed to link %s:%s to %s:%s: %w", e.FromNode, e.FromPort, e.ToNode, e.ToPort, err)
		}
	}

	return nodeList, nil
}
