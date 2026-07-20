package engine

import (
	"context"
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

type FilterAdapter struct {
	engine    FilterEngine
	lifecycle engineLifecycle
	in        *node.InPort[media.Frame]
	out       *node.OutPort[media.Frame]
}

func WrapFilter(engine FilterEngine) node.Filter {
	return &FilterAdapter{
		engine:    engine,
		lifecycle: newEngineLifecycle(engine),
		in:        node.NewInPort[media.Frame]("in"),
		out:       node.NewOutPort[media.Frame]("out", media.StreamInfo{}),
	}
}

func (n *FilterAdapter) Start(ctx context.Context) error {
	in := n.in.Edge()
	out := n.out.Edge()
	if in == nil || out == nil {
		return fmt.Errorf("filter ports not connected")
	}
	return runCodecLoop(ctx, in, out,
		func(f media.Frame) error {
			return n.engine.SendFrame(&f)
		},
		func() (media.Frame, error) {
			f, err := n.engine.ReceiveFrame()
			if err != nil {
				return nil, err
			}
			if f == nil || *f == nil {
				return nil, fmt.Errorf("filter returned nil frame")
			}
			return *f, nil
		},
		n.engine.Flush,
	)
}

func (n *FilterAdapter) Close() error {
	return n.lifecycle.Close()
}

func (n *FilterAdapter) Process(ctx context.Context) error {
	return n.Start(ctx)
}

func (n *FilterAdapter) InputPorts() map[string]*node.InPort[media.Frame] {
	return map[string]*node.InPort[media.Frame]{
		"in": n.in,
	}
}

func (n *FilterAdapter) OutputPorts() map[string]*node.OutPort[media.Frame] {
	return map[string]*node.OutPort[media.Frame]{
		"out": n.out,
	}
}
