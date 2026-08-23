// Package program owns the private, executable result of planning.
package program

import (
	"errors"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/observe"
	"github.com/godexture/godec/internal/run"
	"github.com/godexture/godec/internal/scratch"
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

func Project(compiled graph.Graph, policy job.Policy) (plan.Runtime, error) {
	template, err := compileRuntime(compiled, policy)
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
	template, err := compileRuntime(compiled, public.EffectivePolicy())
	if err != nil {
		return Program{}, err
	}
	if !template.Matches(public.Runtime()) {
		return Program{}, errors.New("program runtime topology differs from Plan")
	}
	result := Program{graph: compiled, plan: public, nodes: nodes, byID: byID, bound: boundaries, runtime: template}
	if _, err := result.Scratch(); err != nil {
		return Program{}, err
	}
	return result, nil
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

func (p Program) Build(ledger *journal.Ledger, operators []flow.Operator) (*run.Execution, error) {
	return p.BuildObserved(ledger, operators, nil)
}

func (p Program) BuildObserved(ledger *journal.Ledger, operators []flow.Operator, observer *observe.Collector) (*run.Execution, error) {
	if !p.Executable() {
		return nil, errors.New("program has no complete typed execution binding")
	}
	return p.runtime.BuildObserved(ledger, operators, observer)
}

// ScratchClaims returns the fixed node-local temporary-byte ceilings selected
// during compilation. Payload resources deliberately remain separate.
func (p Program) ScratchClaims() (map[job.NodeID]resource.Bytes, error) {
	if !p.Valid() {
		return nil, errors.New("program is invalid")
	}
	claims := make(map[job.NodeID]resource.Bytes)
	for _, node := range p.nodes {
		claim := node.Compilation().Scratch()
		if claim != 0 {
			claims[node.ID()] = claim
		}
	}
	return claims, nil
}

// Scratch re-derives the aggregate reservation from private compilation and
// boundary state, then verifies the public Plan projection did not drift.
func (p Program) Scratch() (scratch.Reservation, error) {
	claims, err := p.ScratchClaims()
	if err != nil {
		return scratch.Reservation{}, err
	}
	values := make([]resource.Bytes, 0, len(claims)+len(p.plan.Boundaries()))
	for _, value := range claims {
		values = append(values, value)
	}
	for _, boundary := range p.plan.Boundaries() {
		if boundary.Spool.Valid() {
			values = append(values, resource.Bytes(boundary.Spool.MaximumBytes()))
		}
	}
	reservation, err := scratch.Reserve(p.plan.EffectivePolicy().Resources.ScratchMaxBytes, values...)
	if err != nil {
		return scratch.Reservation{}, err
	}
	projection := p.plan.Scratch()
	if projection.Limit != reservation.Limit() || projection.Reserved != reservation.Reserved() {
		return scratch.Reservation{}, errors.New("program scratch reservation differs from Plan")
	}
	return reservation, nil
}

func compileRuntime(compiled graph.Graph, policy job.Policy) (run.Template, error) {
	nodes := compiled.Nodes()
	values := make([]run.Node, len(nodes))
	for index, node := range nodes {
		execution, _ := plugin.ExecutionOf(node.Compilation())
		values[index] = run.Node{ID: node.ID(), Shape: node.Shape(), Inputs: node.Inputs(), Outputs: node.Outputs(), Execution: execution}
	}
	return run.Compile(values, compiled.Edges(), policy.Resources.Queue, policy.Alignment)
}

// TemporaryClaims returns the ceilings of the node-local stores that grow
// rather than reserve. Nothing has been set aside for them; the Host charges
// what they write against the ceiling the job shares between them.
func (p Program) TemporaryClaims() (map[job.NodeID]resource.Bytes, error) {
	if !p.Valid() {
		return nil, errors.New("program is invalid")
	}
	claims := make(map[job.NodeID]resource.Bytes)
	for _, node := range p.nodes {
		if claim := node.Compilation().Temporary(); claim != 0 {
			claims[node.ID()] = claim
		}
	}
	return claims, nil
}

// TemporaryBudget is the ceiling those stores share, taken from the effective
// policy the Plan was built with.
func (p Program) TemporaryBudget() *scratch.Budget {
	resources := p.plan.EffectivePolicy().Resources
	return scratch.NewBudget(resources.TemporaryMaxBytes, resources.TemporaryUnlimited)
}
