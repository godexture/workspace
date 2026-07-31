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

type packetCursor struct {
	port   *node.InPort[*media.Packet]
	packet *media.Packet
	data   []byte
	eof    bool
}

func NewPacketCompare() *packetCompareNode {
	return &packetCompareNode{
		expected: node.NewInPort[*media.Packet]("expected"),
		actual:   node.NewInPort[*media.Packet]("actual"),
	}
}

func (n *packetCompareNode) InputPorts() map[string]*node.InPort[*media.Packet] {
	return map[string]*node.InPort[*media.Packet]{"expected": n.expected, "actual": n.actual}
}

func (n *packetCompareNode) Start(ctx context.Context) error {
	if n.expected.Edge() == nil || n.actual.Edge() == nil {
		return fmt.Errorf("packet comparator ports not connected")
	}
	expected := packetCursor{port: n.expected}
	actual := packetCursor{port: n.actual}
	defer expected.release()
	defer actual.release()
	byteOffset := 0
	for {
		if err := expected.fill(ctx); err != nil {
			return err
		}
		if err := actual.fill(ctx); err != nil {
			return err
		}
		if expected.eof && actual.eof {
			return nil
		}
		if expected.eof || actual.eof {
			return fmt.Errorf("packet byte stream length mismatch at offset %d", byteOffset)
		}
		compareBytes := min(len(expected.data), len(actual.data))
		if !bytes.Equal(expected.data[:compareBytes], actual.data[:compareBytes]) {
			return fmt.Errorf("packet byte stream mismatch at offset %d", byteOffset)
		}
		expected.consume(compareBytes)
		actual.consume(compareBytes)
		byteOffset += compareBytes
	}
}

func (n *packetCompareNode) Close() error { return nil }

func (c *packetCursor) fill(ctx context.Context) error {
	for len(c.data) == 0 && !c.eof {
		c.release()
		packet, err := c.port.Pull(ctx)
		if errors.Is(err, io.EOF) {
			c.eof = true
			return nil
		}
		if err != nil {
			return err
		}
		if packet == nil {
			return errors.New("packet comparator received nil packet")
		}
		c.packet = packet
		c.data = packet.Data()
	}
	return nil
}

func (c *packetCursor) consume(length int) {
	c.data = c.data[length:]
	if len(c.data) == 0 {
		c.release()
	}
}

func (c *packetCursor) release() {
	if c.packet != nil {
		c.packet.Release()
		c.packet = nil
		c.data = nil
	}
}
