package main

import (
	"context"
	"fmt"
	"os"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/routing"

	_ "github.com/godexture/codec-flac"
	_ "github.com/godexture/codec-mp3"
	_ "github.com/godexture/codec-pcm"
	_ "github.com/godexture/format-flac"
	_ "github.com/godexture/format-mp3"
	wav "github.com/godexture/format-wav"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run . <input> <output.wav>")
		return
	}

	inputPath := os.Args[1]
	outputPath := os.Args[2]

	inputFile, err := os.Open(inputPath)
	if err != nil {
		fmt.Printf("Failed to open input: %v\n", err)
		return
	}
	defer inputFile.Close()

	outputFile, err := os.Create(outputPath)
	if err != nil {
		fmt.Printf("Failed to create output: %v\n", err)
		return
	}
	defer outputFile.Close()

	// 1. Negotiate Geometry
	negotiator := godec.NewNegotiator()

	spec := routing.ConversionSpec{
		Input:       inputFile,
		Output:      outputFile,
		TargetCodec: media.CodecLPCM,
		MuxConfig:   wav.MuxerConfig{},
	}

	geometry, err := negotiator.NegotiateConversion(context.Background(), spec)
	if err != nil {
		fmt.Printf("Failed to negotiate conversion: %v\n", err)
		return
	}

	// 2. Build Pipeline
	builder := godec.NewBuilder()
	nodes, err := builder.Build(geometry)
	if err != nil {
		fmt.Printf("Failed to build pipeline: %v\n", err)
		return
	}

	// 3. Run Pipeline
	runner := godec.NewRunner()
	if err := runner.Run(context.Background(), nodes); err != nil {
		fmt.Printf("Pipeline execution failed: %v\n", err)
		return
	}

	fmt.Printf("Successfully converted %s to %s (%s)\n", inputPath, outputPath, media.CodecLPCM)
}
