package standard

import (
	"context"

	"github.com/godexture/godec/host"
)

// Convert runs one file-to-file conversion with the official composition.
// It returns the host result even when execution reports an error, so callers
// can inspect committed outputs and actual metadata-loss evidence. Setup
// failures return a zero Result. Use NewFileJob and NewHost when the input
// needs explicit media properties or the composition includes third-party
// definitions.
func Convert(ctx context.Context, inputPath, outputPath string) (host.Result, error) {
	request, err := NewFileJob(inputPath, outputPath)
	if err != nil {
		return host.Result{}, err
	}
	instance, err := NewHost()
	if err != nil {
		return host.Result{}, err
	}
	return instance.Run(ctx, request)
}
