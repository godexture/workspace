package engine

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

func (n *FilterAdapter) Start(ctx context.Context) error {
	runPorts := n.runPorts()
	if len(runPorts) == 1 && len(n.outputs) == 1 {
		return n.runSimple(ctx, runPorts[0])
	}
	return n.runGeneral(ctx, runPorts)
}

// runSimple is the smallest topology: one run-phase input, one output,
// nothing beyond the base FilterEngine required. Any preload ports (drained
// separately, before Start ever runs) are irrelevant here.
func (n *FilterAdapter) runSimple(ctx context.Context, inputID string) error {
	in := n.inputs[inputID].Edge()
	out := n.soleOutputEdge()
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
			if frame == nil {
				return nil, fmt.Errorf("filter returned nil frame")
			}
			return frame, nil
		},
		n.engine.Flush,
	)
}

func (n *FilterAdapter) soleOutputEdge() node.Edge[media.Frame] {
	for _, port := range n.outputs {
		return port.Edge()
	}
	return nil
}

func (n *FilterAdapter) runPorts() []string {
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

// runGeneral handles every topology runSimple doesn't: more than one
// run-phase input, and/or more than one output. A single run-phase input
// still goes through here whenever there is more than one output, in which
// case it is sent with the base SendFrame (no AuxInputEngine needed just
// for that); two or more run-phase inputs always require AuxInputEngine.
func (n *FilterAdapter) runGeneral(ctx context.Context, runPorts []string) error {
	outEdges, err := n.outputEdges()
	if err != nil {
		return err
	}
	defer closeEdges(outEdges)

	pullContext, cancel := context.WithCancel(ctx)
	defer cancel()

	inputs := make(chan multiInputResult, len(runPorts))
	var pulls sync.WaitGroup
	for _, port := range runPorts {
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

	singleRun := len(runPorts) == 1
	var aux AuxInputEngine
	if !singleRun {
		var ok bool
		aux, ok = n.engine.(AuxInputEngine)
		if !ok {
			return fmt.Errorf("filter has more than one run-phase input but its engine does not implement AuxInputEngine")
		}
	}

	open := len(runPorts)
	for input := range inputs {
		if input.err == io.EOF {
			if !singleRun {
				if err := aux.EndInput(input.port); err != nil {
					return err
				}
			}
			open--
			if open != 0 {
				continue
			}
			if err := n.engine.Flush(); err != nil {
				return err
			}
			return n.drain(ctx, outEdges, true)
		}
		if input.err != nil {
			return input.err
		}

		var sendErr error
		if singleRun {
			sendErr = n.engine.SendFrame(&input.frame)
		} else {
			sendErr = aux.SendInput(input.port, &input.frame)
		}
		input.frame.Release()
		if sendErr != nil {
			return sendErr
		}
		if err := n.drain(ctx, outEdges, false); err != nil {
			return err
		}
	}
	return fmt.Errorf("filter input streams ended without EOF")
}

func (n *FilterAdapter) drain(ctx context.Context, outEdges map[string]node.Edge[media.Frame], final bool) error {
	if len(outEdges) == 1 {
		var edge node.Edge[media.Frame]
		for _, e := range outEdges {
			edge = e
		}
		for {
			frame, err := n.engine.ReceiveFrame()
			if err == ErrEAGAIN || (final && (err == io.EOF || err == ErrEOF)) {
				return nil
			}
			if err != nil {
				return err
			}
			if frame == nil {
				return fmt.Errorf("filter returned nil frame")
			}
			if err := edge.Push(ctx, frame); err != nil {
				frame.Release()
				return err
			}
		}
	}

	multi, ok := n.engine.(MultiOutputEngine)
	if !ok {
		return fmt.Errorf("filter has more than one output port but its engine does not implement MultiOutputEngine")
	}
	for {
		port, frame, err := multi.ReceiveOutput()
		if err == ErrEAGAIN || (final && (err == io.EOF || err == ErrEOF)) {
			return nil
		}
		if err != nil {
			return err
		}
		if frame == nil {
			return fmt.Errorf("filter returned nil frame")
		}
		edge, ok := outEdges[port]
		if !ok {
			return fmt.Errorf("filter produced output for unknown port %q", port)
		}
		if err := edge.Push(ctx, frame); err != nil {
			frame.Release()
			return err
		}
	}
}

func (n *FilterAdapter) outputEdges() (map[string]node.Edge[media.Frame], error) {
	edges := make(map[string]node.Edge[media.Frame], len(n.outputs))
	for id, port := range n.outputs {
		edge := port.Edge()
		if edge == nil {
			return nil, fmt.Errorf("filter output port %q is not connected", id)
		}
		edges[id] = edge
	}
	return edges, nil
}

func closeEdges(edges map[string]node.Edge[media.Frame]) {
	for _, edge := range edges {
		edge.Close()
	}
}
