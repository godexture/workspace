package config

import (
	"fmt"
	"path/filepath"

	"github.com/godexture/sdk/testutil"
)

type testProfile struct {
	Name                    string
	SnapshotCompareOptions  testutil.CompareOptions
	RoundtripCompareOptions testutil.CompareOptions
}

var Profiles = []testProfile{
	{
		Name:                    "60 - mono audio.flac",
		SnapshotCompareOptions:  flacCompareOptions,
		RoundtripCompareOptions: wavRoundtripCompareOptions,
	},
}

var (
	TestdataDir = "testdata"
	SnapshotDir = filepath.Join(TestdataDir, "snapshots")
)

var (
	flacCompareOptions         = testutil.CompareOptions{MaxAbsDiff: 1.0 / 32768.0, MaxRMSE: 1e-6, MinSNR: 90.0}
	wavRoundtripCompareOptions = testutil.CompareOptions{MaxAbsDiff: 1.0 / 32768.0, MaxRMSE: 2e-5, MinSNR: 80.0}
)

func BuildTestdataPath(fileName string) string {
	return filepath.Join(TestdataDir, fileName)
}

func BuildSnapshotPath(fileName string) string {
	return filepath.Join(SnapshotDir, fileName+".snapshot")
}

func EnumerateTestdataFiles() []string {
	paths, err := filepath.Glob(filepath.Join(TestdataDir, "*.flac"))
	if err != nil {
		panic(fmt.Errorf("failed to glob test files: %v", err))
	}

	fileNames := make([]string, len(paths))
	for i, path := range paths {
		fileNames[i] = filepath.Base(path)
	}

	return fileNames
}

func ProfileForFile(fileName string) testProfile {
	for _, profile := range Profiles {
		if profile.Name == fileName {
			return profile
		}
	}
	panic(fmt.Errorf("missing FLAC test profile for %s", fileName))
}
