// Package profiling provides helpers shared by godexture's CLI and example
// entry points for handling CPU profile output paths.
package profiling

import (
	"fmt"
	"path/filepath"
)

// RejectPathCollision returns an error if profilePath resolves to the same
// absolute path as any of otherPaths. Profile files are typically created
// via os.Create before input/output files are opened, so an aliased path
// would silently truncate one of them.
func RejectPathCollision(profilePath string, otherPaths ...string) error {
	resolvedProfile, err := filepath.Abs(profilePath)
	if err != nil {
		return fmt.Errorf("failed to resolve pprof path %q: %w", profilePath, err)
	}
	for _, path := range otherPaths {
		resolved, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("failed to resolve path %q: %w", path, err)
		}
		if resolvedProfile == resolved {
			return fmt.Errorf("GODEC_PPROF path %q must not match the input or output path", profilePath)
		}
	}
	return nil
}
