// Package surface contains shared normalization used by public application
// surfaces. It owns no Host composition, catalog, planner, or runtime state.
package surface

import (
	"errors"
	"path/filepath"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
)

// FileJob builds a catalog-independent file-to-file request. Nil selectors
// derive extension hints from each path without filesystem I/O.
func FileJob(inputPath string, inputReference access.Reference, outputPath string, outputReference access.Reference, inputSelector, outputSelector *job.FormatSelector, options ...job.Option) (job.Job, error) {
	input, err := job.InputFromReference(inputReference)
	if err != nil {
		return job.Job{}, err
	}
	output, err := job.OutputToReference(outputReference)
	if err != nil {
		return job.Job{}, err
	}
	selectedInput, err := fileSelector(inputPath, inputSelector)
	if err != nil {
		return job.Job{}, err
	}
	if selectedInput.Valid() {
		input, err = input.WithFormatHint(selectedInput)
		if err != nil {
			return job.Job{}, err
		}
	}
	selectedOutput, err := fileSelector(outputPath, outputSelector)
	if err != nil {
		return job.Job{}, err
	}
	if selectedOutput.Valid() {
		output, err = output.WithFormatRequest(selectedOutput)
		if err != nil {
			return job.Job{}, err
		}
	}
	return job.New([]job.Input{input}, []job.Output{output}, job.Graph{}, options...)
}

func fileSelector(path string, explicit *job.FormatSelector) (job.FormatSelector, error) {
	if explicit != nil {
		if !explicit.Valid() {
			return job.FormatSelector{}, errors.New("file Format selector is invalid")
		}
		return *explicit, nil
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
