package standard

import "context"

// Convert runs one file-to-file conversion with the official composition.
// Use NewFileJob and NewHost when the input needs explicit media properties or
// the composition includes third-party definitions.
func Convert(ctx context.Context, inputPath, outputPath string) error {
	request, err := NewFileJob(inputPath, outputPath)
	if err != nil {
		return err
	}
	instance, err := NewHost()
	if err != nil {
		return err
	}
	_, err = instance.Run(ctx, request)
	return err
}
