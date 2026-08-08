package graph

import (
	"errors"
	"sort"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

var ErrGapCardinality = errors.New("input gap does not have exactly one edge and descriptor")

// Gap retains the typed downstream requirement and the exact edge state that
// a bridge must replace.
type Gap struct {
	node      job.NodeID
	port      string
	edge      job.Edge
	hasEdge   bool
	input     stream.Descriptor
	hasInput  bool
	expected  schema.ID
	need      plugin.Need[stream.Descriptor]
	component plugin.Component
	config    config.ResolvedView
	inputs    flow.Descriptors[stream.Descriptor]
}

func (g Gap) Node() job.NodeID                            { return g.node }
func (g Gap) Port() string                                { return g.port }
func (g Gap) Need() plugin.Need[stream.Descriptor]        { return g.need }
func (g Gap) ExpectedSchema() schema.ID                   { return g.expected }
func (g Gap) Component() plugin.Component                 { return g.component }
func (g Gap) Config() config.ResolvedView                 { return g.config }
func (g Gap) Inputs() flow.Descriptors[stream.Descriptor] { return copyDescriptors(g.inputs) }
func (g Gap) Edge() (job.Edge, bool)                      { return g.edge, g.hasEdge }
func (g Gap) Input() (stream.Descriptor, bool)            { return g.input, g.hasInput }

// Accepts confirms a candidate with the same downstream Compile contract used
// by final graph evaluation.
func (g Gap) Accepts(candidate stream.Descriptor) (bool, error) {
	if !g.hasEdge || !g.hasInput {
		return false, ErrGapCardinality
	}
	if !candidate.Valid() || candidate.Schema() != g.expected {
		return false, nil
	}
	bindings := g.inputs.Bindings()
	replaced := 0
	for index, binding := range bindings {
		if binding.Port() == g.port {
			bindings[index] = flow.Describe(g.port, candidate)
			replaced++
		}
	}
	if replaced != 1 {
		return false, ErrGapCardinality
	}
	compilation, err := plugin.Compile(g.component, plugin.CompileContext{}, g.config, flow.NewDescriptors(bindings...))
	if err != nil {
		return false, err
	}
	requirements, ok := plugin.RequirementsOf[stream.Descriptor](compilation)
	if !ok {
		return false, errors.New("downstream compilation returned incompatible requirements")
	}
	for _, requirement := range requirements {
		if requirement.Port() == g.port {
			return false, nil
		}
	}
	return true, nil
}

func gapFor(node shapedNode, edges []job.Edge, compiled map[job.NodeID]Node, component plugin.Component, configValue config.ResolvedView, inputs flow.Descriptors[stream.Descriptor], need plugin.Need[stream.Descriptor], port string) Gap {
	gap := Gap{
		node:      node.request.ID(),
		port:      port,
		expected:  portSchema(node.shape.Inputs, port),
		need:      need,
		component: component,
		config:    configValue,
		inputs:    copyDescriptors(inputs),
	}
	incoming := incomingEdges(edges, node.request.ID(), port)
	if len(incoming) != 1 {
		return gap
	}
	gap.edge = incoming[0]
	gap.hasEdge = true
	upstream, ok := compiled[incoming[0].From().Node()]
	if !ok {
		return gap
	}
	values := upstream.Outputs().At(incoming[0].From().ID())
	if len(values) != 1 {
		return gap
	}
	gap.input = values[0]
	gap.hasInput = true
	return gap
}

func portSchema(ports []flow.Port, id string) schema.ID {
	port, ok := findPort(ports, id)
	if !ok {
		return schema.ID{}
	}
	return port.Schema().Identity()
}

func sortGaps(gaps []Gap) {
	sort.Slice(gaps, func(left, right int) bool {
		if gaps[left].node != gaps[right].node {
			return gaps[left].node.String() < gaps[right].node.String()
		}
		if gaps[left].port != gaps[right].port {
			return gaps[left].port < gaps[right].port
		}
		return gaps[left].need.Code() < gaps[right].need.Code()
	})
}
