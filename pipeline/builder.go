package pipeline

import (
	"fmt"

	"github.com/godexture/core/node"
)

func Link[T any, A node.OutputNode[T], B node.InputNode[T]](nodeA A, portA string, nodeB B, portB string) error {
	outPort, okA := nodeA.OutputPorts()[portA]
	if !okA {
		return fmt.Errorf("output port '%s' not found on node A", portA)
	}

	inPort, okB := nodeB.InputPorts()[portB]
	if !okB {
		return fmt.Errorf("input port '%s' not found on node B", portB)
	}

	edge := NewChanEdge[T](100)

	outPort.Connect(edge)
	inPort.Connect(edge)

	return nil
}
