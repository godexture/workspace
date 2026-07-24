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

// FilterInput names one input port and declares when it is consumed: Run
// ports are pulled during the pipeline's normal run; Preload ports are
// drained to completion before the run begins (see node.StagedInput).
type FilterInput struct {
	ID    string
	Phase node.InputPhase
}

type filterOptions struct {
	inputs  []FilterInput
	outputs []string
}

// FilterOption configures WrapFilter's port topology. Callers that need
// only the conventional single "in"/"out" ports pass no options.
type FilterOption func(*filterOptions)

// WithInputs declares this filter's input ports, replacing the default
// single run-phase "in" port. Declaring more than one run-phase port, or
// any preload port, requires the engine to also implement AuxInputEngine.
func WithInputs(inputs ...FilterInput) FilterOption {
	return func(o *filterOptions) { o.inputs = inputs }
}

// WithOutputs names this filter's output ports, replacing the default
// single "out" port. Declaring more than one requires the engine to also
// implement MultiOutputEngine.
func WithOutputs(outputs ...string) FilterOption {
	return func(o *filterOptions) { o.outputs = outputs }
}

// FilterAdapter adapts a FilterEngine to node.Filter. A single run-phase
// input and a single output is the smallest possible topology and needs
// nothing beyond FilterEngine itself; more of either is a difference in
// degree, not in kind, and is handled by the same adapter type using the
// optional AuxInputEngine/MultiOutputEngine capabilities.
type FilterAdapter struct {
	engine    FilterEngine
	lifecycle engineLifecycle
	inputs    map[string]*node.InPort[media.Frame]
	phases    map[string]node.InputPhase
	outputs   map[string]*node.OutPort[media.Frame]
}

// WrapFilter adapts engine to node.Filter. With no options it exposes the
// conventional single "in" (run-phase) input port and single "out" output
// port, using only SendFrame/ReceiveFrame/Flush.
func WrapFilter(engine FilterEngine, options ...FilterOption) node.Filter {
	var opts filterOptions
	for _, option := range options {
		option(&opts)
	}
	inputs := opts.inputs
	if len(inputs) == 0 {
		inputs = []FilterInput{{ID: "in", Phase: node.InputPhaseRun}}
	}
	outputs := opts.outputs
	if len(outputs) == 0 {
		outputs = []string{"out"}
	}

	inputPorts := make(map[string]*node.InPort[media.Frame], len(inputs))
	phases := make(map[string]node.InputPhase, len(inputs))
	runCount, preloadCount := 0, 0
	for _, input := range inputs {
		if input.ID == "" {
			panic("filter input ID must not be empty")
		}
		if _, exists := inputPorts[input.ID]; exists {
			panic("duplicate filter input ID: " + input.ID)
		}
		inputPorts[input.ID] = node.NewInPort[media.Frame](input.ID)
		phases[input.ID] = input.Phase
		if input.Phase == node.InputPhaseRun {
			runCount++
		} else {
			preloadCount++
		}
	}
	if runCount == 0 {
		panic("filter requires at least one run-phase input")
	}
	if runCount > 1 || preloadCount > 0 {
		if _, ok := engine.(AuxInputEngine); !ok {
			panic("filter declares more than one input port but its engine does not implement AuxInputEngine")
		}
	}

	outputPorts := make(map[string]*node.OutPort[media.Frame], len(outputs))
	for _, id := range outputs {
		if id == "" {
			panic("filter output ID must not be empty")
		}
		if _, exists := outputPorts[id]; exists {
			panic("duplicate filter output ID: " + id)
		}
		outputPorts[id] = node.NewOutPort[media.Frame](id, media.StreamInfo{})
	}
	if len(outputPorts) > 1 {
		if _, ok := engine.(MultiOutputEngine); !ok {
			panic("filter declares more than one output port but its engine does not implement MultiOutputEngine")
		}
	}

	return &FilterAdapter{
		engine:    engine,
		lifecycle: newEngineLifecycle(engine),
		inputs:    inputPorts,
		phases:    phases,
		outputs:   outputPorts,
	}
}

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

// Preload drains every declared preload-phase port to EOF, before Start
// (and thus the rest of the pipeline's run) ever begins.
func (n *FilterAdapter) Preload(ctx context.Context) error {
	ports := make([]string, 0)
	for id, phase := range n.phases {
		if phase == node.InputPhasePreload {
			ports = append(ports, id)
		}
	}
	if len(ports) == 0 {
		return nil
	}
	sort.Strings(ports)
	aux, ok := n.engine.(AuxInputEngine)
	if !ok {
		return fmt.Errorf("filter has a preload input port but its engine does not implement AuxInputEngine")
	}
	for _, id := range ports {
		edge := n.inputs[id].Edge()
		if edge == nil {
			return fmt.Errorf("preload filter port %q not connected", id)
		}
		for {
			frame, err := edge.Pull(ctx)
			if err == io.EOF {
				if err := aux.EndInput(id); err != nil {
					return err
				}
				break
			}
			if err != nil {
				return err
			}
			if err := aux.SendInput(id, &frame); err != nil {
				frame.Release()
				return err
			}
			frame.Release()
		}
	}
	return nil
}

func (n *FilterAdapter) InputPhases() map[string]node.InputPhase {
	result := make(map[string]node.InputPhase, len(n.phases))
	for id, phase := range n.phases {
		result[id] = phase
	}
	return result
}

func (n *FilterAdapter) Prepare(resources registry.ResourceGrant) error {
	return n.lifecycle.Prepare(resources)
}

func (n *FilterAdapter) Close() error                      { return n.lifecycle.Close() }
func (n *FilterAdapter) Process(ctx context.Context) error { return n.Start(ctx) }
func (n *FilterAdapter) InputPorts() map[string]*node.InPort[media.Frame] {
	return n.inputs
}
func (n *FilterAdapter) OutputPorts() map[string]*node.OutPort[media.Frame] {
	return n.outputs
}
