//go:build !windows

package file

import (
	"errors"
	"os"
	"path/filepath"
)

func syncDirectory(target string) error {
	directory, err := os.Open(filepath.Dir(target))
	if err != nil {
		return redactIO("open-directory", err)
	}
	return errors.Join(redactIO("sync-directory", directory.Sync()), redactIO("close-directory", directory.Close()))
}
