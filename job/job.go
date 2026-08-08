// Package job defines immutable user requests and explicit pinned graphs.
package job

import (
	"errors"
	"fmt"

	"github.com/godexture/godec/diagnostic"
)

// Job is a normalized request boundary. Inputs and outputs are optional only
// when an explicit graph already contains its own source and sink nodes.
type Job struct {
	inputs  []Input
	outputs []Output
	graph   Graph
	policy  Policy
	budget  Budget
}

type options struct {
	policy Policy
	budget Budget
}

// Option configures immutable planning requirements.
type Option func(*options)

func WithPolicy(policy Policy) Option { return func(options *options) { options.policy = policy } }
func WithBudget(budget Budget) Option { return func(options *options) { options.budget = budget } }

func New(inputs []Input, outputs []Output, graph Graph, values ...Option) (Job, error) {
	if !graph.Valid() && (len(inputs) == 0 || len(outputs) == 0) {
		return Job{}, errors.New("job needs input and output choices or an explicit graph")
	}
	for index, input := range inputs {
		if !input.Valid() {
			return Job{}, fmt.Errorf("job input %d is invalid", index)
		}
	}
	for index, output := range outputs {
		if !output.Valid() {
			return Job{}, fmt.Errorf("job output %d is invalid", index)
		}
	}
	configuration := options{policy: defaultPolicy(), budget: DefaultBudget()}
	for _, option := range values {
		if option != nil {
			option(&configuration)
		}
	}
	if items := configuration.policy.diagnostics(); len(items) != 0 {
		return Job{}, diagnostic.NewError(items...)
	}
	if !configuration.budget.Valid() {
		return Job{}, errors.New("job planning budget is invalid")
	}
	return Job{inputs: append([]Input(nil), inputs...), outputs: append([]Output(nil), outputs...), graph: graph, policy: configuration.policy, budget: configuration.budget}, nil
}

func (j Job) Valid() bool {
	return (j.graph.Valid() || len(j.inputs) != 0 && len(j.outputs) != 0) && j.policy.Valid() && j.budget.Valid()
}
func (j Job) Inputs() []Input   { return append([]Input(nil), j.inputs...) }
func (j Job) Outputs() []Output { return append([]Output(nil), j.outputs...) }
func (j Job) Policy() Policy    { return j.policy }
func (j Job) Budget() Budget    { return j.budget }
func (j Job) Graph() (Graph, bool) {
	return j.graph, j.graph.Valid()
}
