package engine

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"

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
	out := n.out.Edge()
	if out == nil {
		return fmt.Errorf("filter ports not connected")
	}
	runPorts := n.runPorts()
	if len(runPorts) == 1 {
		in := n.inputs["in"].Edge()
		if in == nil {
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
	return n.runMultiple(ctx, out, runPorts)
}

func (n *MultiFilterAdapter) runPorts() []string {
	ports := make([]string, 0, len(n.inputs))
	for id, phase := range n.phases {
		if phase == node.InputPhaseRun {
			ports = append(ports, id)
		}
	}
	sort.Strings(ports)
	return ports
}

type multiInputResult struct {
	port  string
	frame media.Frame
	err   error
}

func (n *MultiFilterAdapter) runMultiple(ctx context.Context, out node.Edge[media.Frame], ports []string) error {
	defer out.Close()
	pullContext, cancel := context.WithCancel(ctx)
	defer cancel()

	inputs := make(chan multiInputResult, len(ports))
	var pulls sync.WaitGroup
	for _, port := range ports {
		edge := n.inputs[port].Edge()
		if edge == nil {
			return fmt.Errorf("filter input port %q is not connected", port)
		}
		pulls.Add(1)
		go func(port string, edge node.Edge[media.Frame]) {
			defer pulls.Done()
			for {
				frame, err := edge.Pull(pullContext)
				select {
				case inputs <- multiInputResult{port: port, frame: frame, err: err}:
				case <-pullContext.Done():
					if err == nil {
						frame.Release()
					}
					return
				}
				if err != nil {
					return
				}
			}
		}(port, edge)
	}
	go func() {
		pulls.Wait()
		close(inputs)
	}()
	defer func() {
		cancel()
		for input := range inputs {
			if input.err == nil {
				input.frame.Release()
			}
		}
	}()

	open := len(ports)
	for input := range inputs {
		if input.err == io.EOF {
			if err := n.engine.EndInput(input.port); err != nil {
				return err
			}
			open--
			if open != 0 {
				continue
			}
			if err := n.engine.Flush(); err != nil {
				return err
			}
			return n.drain(ctx, out, true)
		}
		if input.err != nil {
			return input.err
		}

		var err error
		if input.port == "in" {
			err = n.engine.SendFrame(&input.frame)
		} else {
			err = n.engine.SendInput(input.port, &input.frame)
		}
		input.frame.Release()
		if err != nil {
			return err
		}
		if err := n.drain(ctx, out, false); err != nil {
			return err
		}
	}
	return fmt.Errorf("filter input streams ended without EOF")
}

func (n *MultiFilterAdapter) drain(ctx context.Context, out node.Edge[media.Frame], final bool) error {
	for {
		frame, err := n.engine.ReceiveFrame()
		if err == ErrEAGAIN || (final && (err == io.EOF || err == ErrEOF)) {
			return nil
		}
		if err != nil {
			return err
		}
		if frame == nil || *frame == nil {
			return fmt.Errorf("filter returned nil frame")
		}
		if err := out.Push(ctx, *frame); err != nil {
			(*frame).Release()
			return err
		}
	}
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
