package config

import (
	"fmt"
	"path/filepath"
)

var (
	TestdataDir = "testdata"
	SnapshotDir = filepath.Join(TestdataDir, "snapshots")
)

func BuildTestdataPath(fileName string) string {
	return filepath.Join(TestdataDir, fileName)
}

func BuildSnapshotPath(fileName string) string {
	return filepath.Join(SnapshotDir, fileName+".snapshot")
}

func EnumerateTestdataFiles() []string {
	paths, err := filepath.Glob(filepath.Join(TestdataDir, "*.mp3"))
	if err != nil {
		panic(fmt.Errorf("failed to glob test files: %v", err))
	}

	fileNames := make([]string, len(paths))
	for i, path := range paths {
		fileNames[i] = filepath.Base(path)
	}

	return fileNames
}
