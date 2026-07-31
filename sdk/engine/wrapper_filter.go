package engine

import (
	"context"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/registry"
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
