// Package program owns the private, executable result of planning.
package program

import (
	"errors"
	"math"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/internal/observe"
	"github.com/godexture/godec/internal/run"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type Program struct {
	graph   graph.Graph
	plan    plan.Plan
	nodes   []graph.Node
	byID    map[job.NodeID]int
	bound   bound.State
	runtime run.Template
}

func Project(compiled graph.Graph) (plan.Runtime, error) {
	template, err := compileRuntime(compiled)
	if err != nil {
		return plan.Runtime{}, err
	}
	return template.Projection(), nil
}

func New(compiled graph.Graph, public plan.Plan, boundaries bound.State) (Program, error) {
	if !compiled.Valid() || !public.Valid() || !boundaries.Valid() {
		return Program{}, errors.New("program requires a compiled graph and valid Plan")
	}
	nodes := compiled.Nodes()
	described := public.Nodes()
	if len(nodes) != len(described) {
		return Program{}, errors.New("program graph and Plan have different node counts")
	}
	byID := make(map[job.NodeID]int, len(nodes))
	planned := make(map[string]string, len(described))
	for _, node := range described {
		planned[node.ID] = node.Component
	}
	for index, node := range nodes {
		component, ok := planned[node.ID().String()]
		if !ok || component != node.Component().String() {
			return Program{}, errors.New("program graph and Plan select different nodes")
		}
		byID[node.ID()] = index
	}
	template, err := compileRuntime(compiled)
	if err != nil {
		return Program{}, err
	}
	if !template.Matches(public.Runtime()) {
		return Program{}, errors.New("program runtime topology differs from Plan")
	}
	return Program{graph: compiled, plan: public, nodes: nodes, byID: byID, bound: boundaries, runtime: template}, nil
}

func (p Program) Valid() bool {
	return p.graph.Valid() && p.plan.Valid() && len(p.nodes) == len(p.byID) && p.bound.Valid() && p.runtime.Valid()
}
func (p Program) Executable() bool        { return p.Valid() && p.runtime.Executable() }
func (p Program) Plan() plan.Plan         { return p.plan }
func (p Program) Nodes() []graph.Node     { return append([]graph.Node(nil), p.nodes...) }
func (p Program) Edges() []job.Edge       { return p.graph.Edges() }
func (p Program) Boundaries() bound.State { return bound.New(p.bound.Entries()...) }
func (p Program) Lookup(id job.NodeID) (graph.Node, bool) {
	index, ok := p.byID[id]
	if !ok {
		return graph.Node{}, false
	}
	return p.nodes[index], true
}

// Open binds only the private Compilation selected for id.
func (p Program) Open(ctx plugin.OpenContext, id job.NodeID) (flow.Operator, error) {
	return p.graph.Open(ctx, id)
}

func (p Program) Build(operators []flow.Operator) (*run.Execution, error) {
	return p.BuildObserved(operators, nil)
}

func (p Program) BuildObserved(operators []flow.Operator, observer *observe.Collector) (*run.Execution, error) {
	if !p.Executable() {
		return nil, errors.New("program has no complete typed execution binding")
	}
	return p.runtime.BuildObserved(operators, observer)
}

// Resources returns the exact coarse reservation required by compiled nodes
// and runtime queue slots. Payloads remain charged to node-local allocators.
func (p Program) Resources() (resource.Request, error) {
	if !p.Valid() {
		return resource.Request{}, errors.New("program is invalid")
	}
	var result resource.Request
	for _, node := range p.nodes {
		if err := addRequest(&result, node.Compilation().Resources()); err != nil {
			return resource.Request{}, err
		}
	}
	runtime, err := p.RuntimeResources()
	if err != nil {
		return resource.Request{}, err
	}
	if err := addRequest(&result, runtime); err != nil {
		return resource.Request{}, err
	}
	return result, nil
}

func (p Program) RuntimeResources() (resource.Request, error) {
	if !p.Valid() {
		return resource.Request{}, errors.New("program is invalid")
	}
	var result resource.Request
	for _, buffer := range p.plan.Runtime().Buffers {
		if buffer.Limit.Items < 0 || uint64(buffer.Limit.Items) > math.MaxUint32 || uint64(result.Queue)+uint64(buffer.Limit.Items) > math.MaxUint32 {
			return resource.Request{}, errors.New("program resource request overflows")
		}
		result.Queue += uint32(buffer.Limit.Items)
	}
	return result, nil
}

func addRequest(total *resource.Request, value resource.Request) error {
	if uint64(total.Memory) > math.MaxUint64-uint64(value.Memory) ||
		uint64(total.Temporary) > math.MaxUint64-uint64(value.Temporary) ||
		uint64(total.Workers)+uint64(value.Workers) > math.MaxUint32 ||
		uint64(total.Queue)+uint64(value.Queue) > math.MaxUint32 {
		return errors.New("program resource request overflows")
	}
	total.Memory += value.Memory
	total.Temporary += value.Temporary
	total.Workers += value.Workers
	total.Queue += value.Queue
	return nil
}

func compileRuntime(compiled graph.Graph) (run.Template, error) {
	nodes := compiled.Nodes()
	values := make([]run.Node, len(nodes))
	for index, node := range nodes {
		execution, _ := plugin.ExecutionOf(node.Compilation())
		values[index] = run.Node{ID: node.ID(), Shape: node.Shape(), Execution: execution}
	}
	return run.Compile(values, compiled.Edges())
}
