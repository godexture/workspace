package nodes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

type packetCompareNode struct {
	expected *node.InPort[*media.Packet]
	actual   *node.InPort[*media.Packet]
}

func NewPacketCompare() *packetCompareNode {
	return &packetCompareNode{
		expected: node.NewInPort[*media.Packet]("expected", nil),
		actual:   node.NewInPort[*media.Packet]("actual", nil),
	}
}

func (n *packetCompareNode) InputPorts() map[string]*node.InPort[*media.Packet] {
	return map[string]*node.InPort[*media.Packet]{"expected": n.expected, "actual": n.actual}
}

func (n *packetCompareNode) Start(ctx context.Context) error {
	if n.expected.Edge() == nil || n.actual.Edge() == nil {
		return fmt.Errorf("packet comparator ports not connected")
	}
	for index := 0; ; index++ {
		expected, expectedErr := n.expected.Pull(ctx)
		actual, actualErr := n.actual.Pull(ctx)
		expectedEOF := errors.Is(expectedErr, io.EOF)
		actualEOF := errors.Is(actualErr, io.EOF)
		if expectedEOF && actualEOF {
			return nil
		}
		if expectedErr != nil && !expectedEOF {
			return expectedErr
		}
		if actualErr != nil && !actualEOF {
			return actualErr
		}
		if expectedEOF || actualEOF {
			return fmt.Errorf("packet count mismatch at packet %d", index)
		}
		if !bytes.Equal(expected.Data(), actual.Data()) {
			return fmt.Errorf("packet %d mismatch", index)
		}
	}
}
