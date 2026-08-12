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
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
