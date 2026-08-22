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
	inputs   []Input
	outputs  []Output
	mappings []Mapping
	graph    Graph
	policy   Policy
	budget   Budget
}

type options struct {
	policy   Policy
	budget   Budget
	mappings []Mapping
}

// Option configures immutable planning requirements.
type Option func(*options)

func WithPolicy(policy Policy) Option { return func(options *options) { options.policy = policy } }
func WithBudget(budget Budget) Option { return func(options *options) { options.budget = budget } }

// WithMappings supplies exact stream mappings between the input and output
// choices. An empty set preserves the default all-stream behavior.
func WithMappings(mappings ...Mapping) Option {
	snapshot := append([]Mapping(nil), mappings...)
	return func(options *options) { options.mappings = append([]Mapping(nil), snapshot...) }
}

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
	mappings, err := normalizeMappings(configuration.mappings, len(inputs), len(outputs))
	if err != nil {
		return Job{}, err
	}
	return Job{inputs: cloneInputs(inputs), outputs: cloneOutputs(outputs), mappings: mappings, graph: graph, policy: configuration.policy, budget: configuration.budget}, nil
}

func (j Job) Valid() bool {
	return (j.graph.Valid() || len(j.inputs) != 0 && len(j.outputs) != 0) && j.policy.Valid() && j.budget.Valid()
}
func (j Job) Inputs() []Input     { return cloneInputs(j.inputs) }
func (j Job) Outputs() []Output   { return cloneOutputs(j.outputs) }
func (j Job) Mappings() []Mapping { return append([]Mapping(nil), j.mappings...) }
func (j Job) Policy() Policy      { return j.policy }
func (j Job) Budget() Budget      { return j.budget }
func (j Job) Graph() (Graph, bool) {
	return j.graph, j.graph.Valid()
}

func cloneInputs(values []Input) []Input {
	result := append([]Input(nil), values...)
	for index := range result {
		result[index].format = result[index].format.clone()
	}
	return result
}

func cloneOutputs(values []Output) []Output {
	result := append([]Output(nil), values...)
	for index := range result {
		result[index].format = result[index].format.clone()
	}
	return result
}

func normalizeMappings(values []Mapping, inputCount, outputCount int) ([]Mapping, error) {
	seen := make(map[Mapping]struct{}, len(values))
	for index, mapping := range values {
		if !mapping.Valid() {
			return nil, fmt.Errorf("job mapping %d is invalid", index)
		}
		if mapping.input >= inputCount {
			return nil, fmt.Errorf("job mapping %d input index %d is out of range", index, mapping.input)
		}
		if mapping.output >= outputCount {
			return nil, fmt.Errorf("job mapping %d output index %d is out of range", index, mapping.output)
		}
		if _, exists := seen[mapping]; exists {
			return nil, fmt.Errorf("job mapping %d is repeated", index)
		}
		seen[mapping] = struct{}{}
	}
	return cloneMappings(values), nil
}
