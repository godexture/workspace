package audio

import (
	"context"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

// PCMCollectorNode is a pipeline node that collects decoded audio frames into a float32 slice.
type PCMCollectorNode struct {
	in  *node.InPort[media.Frame]
	pcm []float32
}

func NewPCMCollectorNode() *PCMCollectorNode {
	return &PCMCollectorNode{
		in: node.NewInPort[media.Frame]("in", nil),
	}
}

func (n *PCMCollectorNode) Start(ctx context.Context) error {
	inEdge := n.in.Edge()
	if inEdge == nil {
		return fmt.Errorf("collector input not connected")
	}

	for {
		frame, err := inEdge.Pull(ctx)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if audioFrame, ok := frame.(*media.AudioFrame); ok {
			samples, err := ConvertToFloat32(audioFrame)
			if err != nil {
				return err
			}
			n.pcm = append(n.pcm, samples...)
		} else {
			return fmt.Errorf("expected AudioFrame, got %T", frame)
		}
	}
}

func (n *PCMCollectorNode) InputPorts() map[string]*node.InPort[media.Frame] {
	return map[string]*node.InPort[media.Frame]{"in": n.in}
}

func (n *PCMCollectorNode) PCM() []float32 {
	return n.pcm
}
