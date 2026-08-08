// Package program owns the private, executable result of planning.
package program

import (
	"errors"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/internal/run"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
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

func compileRuntime(compiled graph.Graph) (run.Template, error) {
	nodes := compiled.Nodes()
	values := make([]run.Node, len(nodes))
	for index, node := range nodes {
		execution, _ := plugin.ExecutionOf(node.Compilation())
		values[index] = run.Node{ID: node.ID(), Shape: node.Shape(), Execution: execution}
	}
	return run.Compile(values, compiled.Edges())
}
