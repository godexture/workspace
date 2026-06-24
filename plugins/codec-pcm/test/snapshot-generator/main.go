package main

import (
	"fmt"
	"os"

	"github.com/godexture/codec-pcm/test/config"
	"github.com/godexture/sdk/testutil"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: snapshot-generator <input_dir>")
		os.Exit(1)
	}

	for _, profile := range config.Profiles {
		fmt.Printf("Generating snapshot for %s...\n", profile.Name)

		sourceFile, err := os.Open(config.SourcePath)
		if err != nil {
			fmt.Printf("Failed to open source file %s: %v\n", config.SourcePath, err)
			os.Exit(1)
		}
		defer sourceFile.Close()

		// Test data
		testBytes, err := testutil.EncodeWithFFmpeg(sourceFile, profile.Codec, profile.Attrs)
		if err != nil {
			fmt.Printf("Failed to encode %s: %v\n", profile.Name, err)
			os.Exit(1)
		}

		testPath := config.BuildTestdataPath(profile.Name)
		if err := os.WriteFile(testPath, testBytes, 0644); err != nil {
			fmt.Printf("Failed to write file %s: %v\n", testPath, err)
			os.Exit(1)
		}

		// Decoder snapshots
		testFile, err := os.Open(testPath)
		if err != nil {
			fmt.Printf("Failed to open test file %s: %v\n", testPath, err)
			os.Exit(1)
		}
		defer testFile.Close()

		decodedPCM, err := testutil.DecodeWithFFmpeg(testFile)
		if err != nil {
			fmt.Printf("Failed to decode %s with FFmpeg: %v\n", profile.Name, err)
			os.Exit(1)
		}

		snapshotPath := config.BuildSnapshotPath(profile.Name)
		if err := testutil.SaveSnapshot(snapshotPath, decodedPCM); err != nil {
			fmt.Printf("Failed to save snapshot %s: %v\n", snapshotPath, err)
			os.Exit(1)
		}
	}
}
