package standard

import (
	"github.com/godexture/godec/internal/surface"
	"github.com/godexture/godec/job"
)

// FileJobOption configures one catalog-independent file request.
type FileJobOption func(*fileJobOptions)

type fileJobOptions struct {
	input     job.FormatSelector
	inputSet  bool
	output    job.FormatSelector
	outputSet bool
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
	return surface.FileJob(inputPath, outputPath, input, output)
}
