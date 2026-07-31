package test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godexture/godec/plugins/codec-flac/test/config"
)

func walTestFiles(t *testing.T, run func(t *testing.T, path string, group string)) {
	root := config.TestdataDir
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".flac" {
			return nil
		}

		group := filepath.Dir(path)

		relPath, _ := filepath.Rel(root, path)
		testName := strings.ReplaceAll(relPath, string(os.PathSeparator), "/")

		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			run(t, path, group)
		})
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk testdata: %v", err)
	}
}
