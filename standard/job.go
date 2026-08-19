package standard

import (
	"github.com/godexture/godec/internal/surface"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plugin/file"
)

// FileJobOption configures one catalog-independent file request.
type FileJobOption func(*fileJobOptions)

type fileJobOptions struct {
	input       job.FormatSelector
	inputSet    bool
	output      job.FormatSelector
	outputSet   bool
	mappings    []job.Mapping
	mappingsSet bool
	policy      job.Policy
	policySet   bool
	budget      job.Budget
	budgetSet   bool
}

// WithInputFormat supplies an explicit input Format hint and optional config.
func WithInputFormat(selector job.FormatSelector) FileJobOption {
	return func(options *fileJobOptions) {
		options.input = selector
		options.inputSet = true
	}
}

// WithOutputFormat supplies an explicit output Format request and optional config.
func WithOutputFormat(selector job.FormatSelector) FileJobOption {
	return func(options *fileJobOptions) {
		options.output = selector
		options.outputSet = true
	}
}

// WithMappings supplies exact stream mappings for the file request.
func WithMappings(values ...job.Mapping) FileJobOption {
	snapshot := append([]job.Mapping(nil), values...)
	return func(options *fileJobOptions) {
		options.mappings = append([]job.Mapping(nil), snapshot...)
		options.mappingsSet = true
	}
}

// WithPolicy supplies the policy used to plan the file request.
func WithPolicy(policy job.Policy) FileJobOption {
	return func(options *fileJobOptions) {
		options.policy = policy
		options.policySet = true
	}
}

// WithBudget supplies the bounded planning budget for the file request.
func WithBudget(budget job.Budget) FileJobOption {
	return func(options *fileJobOptions) {
		options.budget = budget
		options.budgetSet = true
	}
}

// NewFileJob builds a catalog-independent file-to-file request. Extensions
// are retained as hints and requests; the receiving Host resolves them.
func NewFileJob(inputPath, outputPath string, values ...FileJobOption) (job.Job, error) {
	options := fileJobOptions{}
	for _, option := range values {
		if option != nil {
			option(&options)
		}
	}
	var input, output *job.FormatSelector
	if options.inputSet {
		input = &options.input
	}
	if options.outputSet {
		output = &options.output
	}
	inputReference, err := file.Reference(inputPath)
	if err != nil {
		return job.Job{}, err
	}
	outputReference, err := file.Reference(outputPath)
	if err != nil {
		return job.Job{}, err
	}
	var jobOptions []job.Option
	if options.policySet {
		jobOptions = append(jobOptions, job.WithPolicy(options.policy))
	}
	if options.budgetSet {
		jobOptions = append(jobOptions, job.WithBudget(options.budget))
	}
	if options.mappingsSet {
		jobOptions = append(jobOptions, job.WithMappings(options.mappings...))
	}
	return surface.FileJob(inputPath, inputReference, outputPath, outputReference, input, output, jobOptions...)
}
