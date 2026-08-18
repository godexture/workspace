package plugin

import (
	"strings"

	"github.com/godexture/godec/flow"
)

// Demand identifies one descriptor condition a component's Suggest function
// may help satisfy. Input demands describe the component's own input ports;
// output demands describe a desired descriptor on one of its output ports.
// The direction is part of the value so the two cases cannot be confused by a
// caller or by a planner bridge.
type Demand[D any] struct {
	direction flow.Direction
	port      string
	need      Need[D]
}

func InputDemand[D any](port string, need Need[D]) Demand[D] {
	return Demand[D]{direction: flow.InputDirection, port: strings.TrimSpace(port), need: need}
}

func OutputDemand[D any](port string, need Need[D]) Demand[D] {
	return Demand[D]{direction: flow.OutputDirection, port: strings.TrimSpace(port), need: need}
}

func (d Demand[D]) Direction() flow.Direction { return d.direction }
func (d Demand[D]) Port() string              { return d.port }
func (d Demand[D]) Need() Need[D]             { return d.need }

func (d Demand[D]) Valid() bool {
	return (d.direction == flow.InputDirection || d.direction == flow.OutputDirection) && d.port != "" && d.need.Valid()
}

// Suggestion is the immutable, ordered input and descriptor-demand view
// passed to a component's Suggest function. Inputs and demands are copied at
// construction and at every accessor boundary.
type Suggestion[D any] struct {
	inputs  flow.Descriptors[D]
	demands []Demand[D]
}

func NewSuggestion[D any](inputs flow.Descriptors[D], demands ...Demand[D]) Suggestion[D] {
	return Suggestion[D]{
		inputs:  flow.NewDescriptors(inputs.Bindings()...),
		demands: append([]Demand[D](nil), demands...),
	}
}

func (s Suggestion[D]) Inputs() flow.Descriptors[D] {
	return flow.NewDescriptors(s.inputs.Bindings()...)
}

func (s Suggestion[D]) Demands() []Demand[D] {
	return append([]Demand[D](nil), s.demands...)
}

func cloneSuggestion[D any](value Suggestion[D]) Suggestion[D] {
	return NewSuggestion(value.Inputs(), value.demands...)
}
