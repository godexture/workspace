package engine

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
)

type FilterInput struct {
	ID    string
	Phase node.InputPhase
}

type MultiFilterAdapter struct {
	engine    MultiFilterEngine
	lifecycle engineLifecycle
	inputs    map[string]*node.InPort[media.Frame]
	phases    map[string]node.InputPhase
	out       *node.OutPort[media.Frame]
}

func WrapMultiFilter(engine MultiFilterEngine, inputs ...FilterInput) node.Filter {
	ports := make(map[string]*node.InPort[media.Frame], len(inputs))
	phases := make(map[string]node.InputPhase, len(inputs))
	for _, input := range inputs {
		if input.ID == "" {
			panic("multi-filter input ID must not be empty")
		}
		if _, exists := ports[input.ID]; exists {
			panic("duplicate multi-filter input ID: " + input.ID)
		}
		ports[input.ID] = node.NewInPort[media.Frame](input.ID)
		phases[input.ID] = input.Phase
	}
	if phases["in"] != node.InputPhaseRun {
		panic("multi-filter requires a run-phase in port")
	}
	return &MultiFilterAdapter{
		engine:    engine,
		lifecycle: newEngineLifecycle(engine),
		inputs:    ports,
		phases:    phases,
		out:       node.NewOutPort[media.Frame]("out", media.StreamInfo{}),
	}
}

func (n *MultiFilterAdapter) Start(ctx context.Context) error {
	for id, phase := range n.phases {
		if id != "in" && phase == node.InputPhaseRun {
			return fmt.Errorf("multi-filter run port %q requires a stream scheduler", id)
		}
	}
	in := n.inputs["in"].Edge()
	out := n.out.Edge()
	if in == nil || out == nil {
		return fmt.Errorf("filter ports not connected")
	}
	return runCodecLoop(ctx, in, out,
		func(frame media.Frame) error { return n.engine.SendFrame(&frame) },
		func() (media.Frame, error) {
			frame, err := n.engine.ReceiveFrame()
			if err != nil {
				return nil, err
			}
			if frame == nil || *frame == nil {
				return nil, fmt.Errorf("filter returned nil frame")
			}
			return *frame, nil
		},
		func() error {
			if err := n.engine.EndInput("in"); err != nil {
				return err
			}
			return n.engine.Flush()
		},
	)
}

func (n *MultiFilterAdapter) Preload(ctx context.Context) error {
	ports := make([]string, 0)
	for id, phase := range n.phases {
		if phase == node.InputPhasePreload {
			ports = append(ports, id)
		}
	}
	sort.Strings(ports)
	for _, id := range ports {
		edge := n.inputs[id].Edge()
		if edge == nil {
			return fmt.Errorf("preload filter port %q not connected", id)
		}
		for {
			frame, err := edge.Pull(ctx)
			if err == io.EOF {
				if err := n.engine.EndInput(id); err != nil {
					return err
				}
				break
			}
			if err != nil {
				return err
			}
			if err := n.engine.SendInput(id, &frame); err != nil {
				frame.Release()
				return err
			}
			frame.Release()
		}
	}
	return nil
}

func (n *MultiFilterAdapter) Prepare(resources registry.ResourceGrant) error {
	return n.lifecycle.Prepare(resources)
}

func (n *MultiFilterAdapter) Close() error                                     { return n.lifecycle.Close() }
func (n *MultiFilterAdapter) Process(ctx context.Context) error                { return n.Start(ctx) }
func (n *MultiFilterAdapter) InputPorts() map[string]*node.InPort[media.Frame] { return n.inputs }
func (n *MultiFilterAdapter) OutputPorts() map[string]*node.OutPort[media.Frame] {
	return map[string]*node.OutPort[media.Frame]{"out": n.out}
}
func (n *MultiFilterAdapter) InputPhases() map[string]node.InputPhase {
	result := make(map[string]node.InputPhase, len(n.phases))
	for id, phase := range n.phases {
		result[id] = phase
	}
	return result
}
