// Package solve performs bounded, deterministic bridge insertion for an
// explicit requested graph.
package solve

import (
	"context"
	"encoding/hex"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/internal/planning"
	"github.com/godexture/godec/internal/program"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
)

type annotation struct {
	origin      plan.Origin
	reason      string
	summary     config.Summary
	inferConfig bool
}

type planner struct {
	context     context.Context
	index       catalog.Index
	request     job.Job
	policy      job.Policy
	budget      job.Budget
	platform    plan.Platform
	usage       plan.Usage
	candidates  candidateIndex
	cache       compileCache
	environment string
	nodes       map[job.NodeID]annotation
	edges       map[string]annotation
	formats     map[formatBoundary]job.NodeID
	bound       bound.State
	contexts    graph.CompileContexts
	warnings    []string
}

// Resolve returns a private Program whose public Plan contains every selected
// requested and automatic node.
func Resolve(ctx context.Context, index catalog.Index, request job.Job, platform plan.Platform) (program.Program, error) {
	return ResolveBound(ctx, index, request, platform, bound.State{}, graph.CompileContexts{})
}

// ResolveBound plans a Job whose Access/Endpoint choices have already been
// normalized into graph nodes by internal/bind.
func ResolveBound(ctx context.Context, index catalog.Index, request job.Job, platform plan.Platform, boundaries bound.State, contexts graph.CompileContexts) (program.Program, error) {
	return resolveBound(ctx, index, request, platform, boundaries, contexts, Preselection{})
}

// ResolvePrepared plans a graph containing Format nodes selected by Prepare
// before ordinary solver gap filling.
func ResolvePrepared(ctx context.Context, index catalog.Index, request job.Job, platform plan.Platform, boundaries bound.State, contexts graph.CompileContexts, selected Preselection) (program.Program, error) {
	return resolveBound(ctx, index, request, platform, boundaries, contexts, selected)
}

func resolveBound(ctx context.Context, index catalog.Index, request job.Job, platform plan.Platform, boundaries bound.State, contexts graph.CompileContexts, selected Preselection) (program.Program, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !request.Valid() || !platform.Valid() || !boundaries.Ready() {
		return program.Program{}, solveDiagnostic("solve.invalid-request", nil, plan.Usage{}, request.Budget(), "invalid", nil)
	}
	requested, ok := request.Graph()
	if !ok {
		return program.Program{}, solveDiagnostic("solve.binding-unavailable", nil, plan.Usage{}, request.Budget(), "binding", nil)
	}
	if !selected.validFor(requested, boundaries.Projections()) {
		return program.Program{}, solveDiagnostic("solve.invalid-request", nil, selected.usage, request.Budget(), "preselection", nil)
	}
	if err := validateRequestedContracts(index, requested, request.Policy(), platform); err != nil {
		return program.Program{}, err
	}
	planningContext, cancel := planning.Start(ctx, request.Budget().Duration)
	defer cancel()
	p := &planner{
		context:  planningContext,
		index:    index,
		request:  request,
		policy:   request.Policy(),
		budget:   request.Budget(),
		platform: platform,
		cache:    make(compileCache),
		nodes:    make(map[job.NodeID]annotation),
		edges:    make(map[string]annotation),
		formats:  make(map[formatBoundary]job.NodeID, len(selected.formats)),
		bound:    boundaries,
		contexts: contexts,
		usage:    selected.usage,
		warnings: append([]string(nil), selected.warnings...),
	}
	for boundary, node := range selected.formats {
		p.formats[boundary] = node
	}
	p.environment = environmentFingerprint(p.policy, platform)
	p.candidates = buildCandidateIndex(index, p.policy, platform)
	for _, node := range requested.Nodes() {
		if selectedNode, automatic := selected.nodes[node.ID()]; automatic {
			p.nodes[node.ID()] = annotation{origin: plan.Automatic, reason: selectedNode.reason, inferConfig: selectedNode.inferConfig}
		} else {
			p.nodes[node.ID()] = annotation{origin: plan.Requested}
		}
	}
	for _, edge := range requested.Edges() {
		if reason, automatic := selected.edges[edgeKey(edge)]; automatic {
			p.edges[edgeKey(edge)] = annotation{origin: plan.Automatic, reason: reason}
		} else {
			p.edges[edgeKey(edge)] = annotation{origin: plan.Requested}
		}
	}

	current := requested
	var lastGap *graph.Gap
	for {
		if err := p.checkContext(); err != nil {
			return program.Program{}, p.planningError(err, lastGap, nil)
		}
		if p.usage.FixpointIterations >= p.budget.FixpointIterations {
			return program.Program{}, p.planningError(limitError{dimension: "fixpoints"}, lastGap, nil)
		}
		p.usage.FixpointIterations++
		evaluation, err := graph.EvaluateBounded(p.index, current, p.contexts.WithContext(p.context), p.beforeCompile)
		if err != nil {
			if contextErr := p.checkContext(); contextErr != nil {
				err = contextErr
			}
			return program.Program{}, p.planningError(err, lastGap, nil)
		}
		if compiled, complete := evaluation.Graph(); complete {
			return p.buildProgram(compiled)
		}
		gaps := evaluation.Gaps()
		if len(gaps) == 0 {
			return program.Program{}, solveDiagnostic("solve.unsupported", nil, p.usage, p.budget, "incomplete", nil)
		}
		gap := gaps[0]
		lastGap = &gap
		result, rejections, err := p.search(gap)
		if err != nil {
			return program.Program{}, p.planningError(err, &gap, rejections)
		}
		if !result.progress() {
			return program.Program{}, solveDiagnostic("solve.nondeterministic", &gap, p.usage, p.budget, "zero-progress", rejections)
		}
		current, err = p.insert(current, gap, result)
		if err != nil {
			return program.Program{}, err
		}
	}
}

func validateRequestedContracts(index catalog.Index, requested job.Graph, policy job.Policy, platform plan.Platform) error {
	for _, node := range requested.Nodes() {
		component, ok := index.Lookup(node.Component())
		if !ok {
			continue
		}
		if !compatibleContract(component.Contract(), policy, platform) {
			return diagnostic.NewError(diagnostic.NewItem("solve.unsupported", diagnostic.ErrorSeverity, diagnostic.Path{Component: node.ID().String()}, "requested component does not satisfy the planning policy", map[string]string{"dimension": "policy", "component": component.Identity().String()}))
		}
	}
	return nil
}

func (p *planner) beforeCompile() error {
	if err := p.checkContext(); err != nil {
		return err
	}
	if p.usage.Compiles >= p.budget.Compiles {
		return limitError{dimension: "compiles"}
	}
	p.usage.Compiles++
	return nil
}

func (p *planner) checkContext() error {
	if err := p.context.Err(); err != nil {
		if planning.DurationExhausted(p.context) {
			return limitError{dimension: "duration"}
		}
		return canceledError{cause: context.Cause(p.context)}
	}
	return nil
}

func catalogFingerprint(index catalog.Index) string {
	fingerprint := index.Fingerprint()
	return hex.EncodeToString(fingerprint[:])
}
