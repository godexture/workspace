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

// Gap retains the typed downstream requirement and the exact edge state that
// a bridge must replace.
type Gap struct {
	node      job.NodeID
	port      string
	edge      job.Edge
	hasEdge   bool
	input     stream.Descriptor
	hasInput  bool
	expected  schema.Descriptor
	need      plugin.Need[stream.Descriptor]
	component plugin.Component
	config    config.ResolvedView
	context   plugin.CompileContext
	inputs    flow.Descriptors[stream.Descriptor]
}

func (g Gap) Node() job.NodeID                            { return g.node }
func (g Gap) Port() string                                { return g.port }
func (g Gap) Need() plugin.Need[stream.Descriptor]        { return g.need }
func (g Gap) ExpectedDescriptor() schema.Descriptor       { return g.expected }
func (g Gap) Component() plugin.Component                 { return g.component }
func (g Gap) Config() config.ResolvedView                 { return g.config }
func (g Gap) Inputs() flow.Descriptors[stream.Descriptor] { return copyDescriptors(g.inputs) }
func (g Gap) Edge() (job.Edge, bool)                      { return g.edge, g.hasEdge }
func (g Gap) Input() (stream.Descriptor, bool)            { return g.input, g.hasInput }

// WithCandidate replaces the sole descriptor feeding this gap's port. A
// route bridge is deliberately unavailable when the port has more than one
// descriptor; fixed-node config inference can still Compile its whole input
// sequence through Compile.
func (g Gap) WithCandidate(candidate stream.Descriptor) (flow.Descriptors[stream.Descriptor], bool) {
	if !candidate.Valid() || !candidate.SchemaDescriptor().Equal(g.expected) {
		return flow.Descriptors[stream.Descriptor]{}, false
	}
	bindings := g.inputs.Bindings()
	replaced := 0
	for index, binding := range bindings {
		if binding.Port() != g.port {
			continue
		}
		bindings[index] = flow.Describe(g.port, candidate)
		replaced++
	}
	if replaced != 1 {
		return flow.Descriptors[stream.Descriptor]{}, false
	}
	return flow.NewDescriptors(bindings...), true
}

// Compile applies a candidate config to the complete ordered input sequence.
// It is the same Compile contract graph evaluation uses, including inputs on
// other ports and every descriptor of a many port.
func (g Gap) Compile(configValue config.ResolvedView, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compilation, []plugin.Requirement[stream.Descriptor], error) {
	compilation, err := plugin.Compile(g.component, g.context, configValue, inputs)
	if err != nil {
		return plugin.Compilation{}, nil, err
	}
	requirements, ok := plugin.RequirementsOf[stream.Descriptor](compilation)
	if !ok {
		return plugin.Compilation{}, nil, errors.New("downstream compilation returned incompatible requirements")
	}
	return compilation, requirements, nil
}

func gapFor(node shapedNode, edges []job.Edge, compiled map[job.NodeID]Node, component plugin.Component, configValue config.ResolvedView, compileContext plugin.CompileContext, inputs flow.Descriptors[stream.Descriptor], need plugin.Need[stream.Descriptor], port string) Gap {
	gap := Gap{
		node:      node.request.ID(),
		port:      port,
		expected:  portDescriptor(node.shape.Inputs, port),
		need:      need,
		component: component,
		config:    configValue,
		context:   compileContext,
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

func portDescriptor(ports []flow.Port, id string) schema.Descriptor {
	port, ok := findPort(ports, id)
	if !ok {
		return schema.Descriptor{}
	}
	return port.Schema()
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
