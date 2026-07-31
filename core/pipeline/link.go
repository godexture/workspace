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

	return connectPorts(outPort, inPort, NewChanEdge[T](bufferSize))
}

func connectPorts[T any](outPort *node.OutPort[T], inPort *node.InPort[T], edge node.Edge[T]) error {
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

func linkAnyConfigured(nodeA node.Node, portA string, nodeB node.Node, portB string, metrics *edgeMetrics, progressOnly bool) error {
	if metrics == nil {
		return LinkAny(nodeA, portA, nodeB, portB)
	}
	if outA, okA := nodeA.(node.OutputNode[*media.Packet]); okA {
		if inB, okB := nodeB.(node.InputNode[*media.Packet]); okB {
			if progressOnly {
				return linkProgress(outA, portA, inB, portB, metrics)
			}
			return linkObserved(outA, portA, inB, portB, metrics)
		}
	}
	if outA, okA := nodeA.(node.OutputNode[media.Frame]); okA {
		if inB, okB := nodeB.(node.InputNode[media.Frame]); okB {
			if progressOnly {
				return linkProgress(outA, portA, inB, portB, metrics)
			}
			return linkObserved(outA, portA, inB, portB, metrics)
		}
	}
	return fmt.Errorf("incompatible nodes or port types: %T and %T", nodeA, nodeB)
}

func linkProgress[T any, A node.OutputNode[T], B node.InputNode[T]](nodeA A, portA string, nodeB B, portB string, metrics *edgeMetrics) error {
	outPort, ok := nodeA.OutputPorts()[portA]
	if !ok {
		return fmt.Errorf("output port '%s' not found on node A", portA)
	}
	inPort, ok := nodeB.InputPorts()[portB]
	if !ok {
		return fmt.Errorf("input port '%s' not found on node B", portB)
	}
	edge := &progressEdge[T]{ChanEdge: NewChanEdge[T](100), metrics: metrics}
	return connectPorts(outPort, inPort, edge)
}

func linkObserved[T any, A node.OutputNode[T], B node.InputNode[T]](nodeA A, portA string, nodeB B, portB string, metrics *edgeMetrics) error {
	outPort, ok := nodeA.OutputPorts()[portA]
	if !ok {
		return fmt.Errorf("output port '%s' not found on node A", portA)
	}
	inPort, ok := nodeB.InputPorts()[portB]
	if !ok {
		return fmt.Errorf("input port '%s' not found on node B", portB)
	}
	edge := &observedEdge[T]{ChanEdge: NewChanEdge[T](100), metrics: metrics}
	return connectPorts(outPort, inPort, edge)
}
