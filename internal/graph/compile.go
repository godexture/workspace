package graph

import (
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/job"
)

// Compile resolves and validates a graph that must already be complete.
func Compile(index catalog.Index, requested job.Graph) (Graph, error) {
	evaluation, err := evaluate(index, requested, false)
	if err != nil {
		return Graph{}, err
	}
	compiled, ok := evaluation.Graph()
	if !ok {
		return Graph{}, diagnostic.NewError(diagnostic.NewItem("graph.requirement", diagnostic.ErrorSeverity, diagnostic.Path{}, "graph has unresolved input requirements", nil))
	}
	return compiled, nil
}

// Evaluate compiles every currently satisfiable node and returns typed gaps
// for the solver. It never opens component implementations.
func Evaluate(index catalog.Index, requested job.Graph) (Evaluation, error) {
	return evaluate(index, requested, true)
}
