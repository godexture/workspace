package standard

import (
	"errors"
	"path/filepath"

	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin/file"
)

type FileJobOption func(*fileJobOptions)

type fileJobOptions struct {
	input     job.FormatSelector
	inputSet  bool
	output    job.FormatSelector
	outputSet bool
}

func WithInputFormat(selector job.FormatSelector) FileJobOption {
	return func(options *fileJobOptions) {
		options.input = selector
		options.inputSet = true
	}
}

func WithOutputFormat(selector job.FormatSelector) FileJobOption {
	return func(options *fileJobOptions) {
		options.output = selector
		options.outputSet = true
	}
}

// NewFileJob builds a catalog-independent file-to-file request. Extensions
// are retained as hints and requests; the receiving Host resolves them.
func NewFileJob(inputPath, outputPath string, values ...FileJobOption) (job.Job, error) {
	inputReference, err := file.Reference(inputPath)
	if err != nil {
		return job.Job{}, err
	}
	outputReference, err := file.Reference(outputPath)
	if err != nil {
		return job.Job{}, err
	}
	input, err := job.InputFromReference(inputReference)
	if err != nil {
		return job.Job{}, err
	}
	output, err := job.OutputToReference(outputReference)
	if err != nil {
		return job.Job{}, err
	}
	options := fileJobOptions{}
	for _, option := range values {
		if option != nil {
			option(&options)
		}
	}
	inputSelector, err := pathSelector(inputPath, options.input, options.inputSet)
	if err != nil {
		return job.Job{}, err
	}
	if inputSelector.Valid() {
		input, err = input.WithFormatHint(inputSelector)
		if err != nil {
			return job.Job{}, err
		}
	}
	outputSelector, err := pathSelector(outputPath, options.output, options.outputSet)
	if err != nil {
		return job.Job{}, err
	}
	if outputSelector.Valid() {
		output, err = output.WithFormatRequest(outputSelector)
		if err != nil {
			return job.Job{}, err
		}
	}
	return job.New([]job.Input{input}, []job.Output{output}, job.Graph{})
}

func pathSelector(path string, explicit job.FormatSelector, explicitSet bool) (job.FormatSelector, error) {
	if explicitSet {
		if !explicit.Valid() {
			return job.FormatSelector{}, errors.New("standard file Format selector is invalid")
		}
		return explicit, nil
	}
	extension := filepath.Ext(path)
	if extension == "" {
		return job.FormatSelector{}, nil
	}
	parsed, err := mediaformat.ParseExtension(extension)
	if err != nil {
		return job.FormatSelector{}, err
	}
	return job.SelectFormatExtension(parsed)
}
