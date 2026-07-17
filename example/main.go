package main

import (
	"context"
	"fmt"
	"os"
	"runtime/pprof"

	"github.com/godexture/sdk/timer"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/routing"

	flacFormat "github.com/godexture/format-flac"

	_ "github.com/godexture/codec-flac"
	_ "github.com/godexture/codec-mp3"
	_ "github.com/godexture/codec-pcm"
	_ "github.com/godexture/format-flac"
	_ "github.com/godexture/format-mp3"
	_ "github.com/godexture/format-wav"
)

const targetCodec = media.CodecFLAC

func main() {
	f, err := os.Create("cpu.pprof")
	if err != nil {
		panic(err)
	}

	if err := pprof.StartCPUProfile(f); err != nil {
		panic(err)
	}
	defer pprof.StopCPUProfile()

	if len(os.Args) <= 2 {
		fmt.Println("Usage: go run . <input> <output.wav>")
		return
	}

	inputPath := os.Args[1]
	outputPath := os.Args[2]

	if profilePath := os.Getenv("GODEC_CPUPROFILE"); profilePath != "" {
		profileFile, err := os.Create(profilePath)
		if err != nil {
			fmt.Printf("Failed to create profile: %v\n", err)
			return
		}
		defer profileFile.Close()
		pprof.StartCPUProfile(profileFile)
		defer pprof.StopCPUProfile()
	}

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
		TargetCodec: targetCodec,
		MuxConfig:   flacFormat.MuxerConfig{},
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

	fmt.Printf("Starting conversion from %s to %s (%s)\n", inputPath, outputPath, targetCodec)
	timer := timer.New()

	if err := runner.Run(context.Background(), nodes); err != nil {
		fmt.Printf("Pipeline execution failed: %v\n", err)
		return
	}

	fmt.Printf("Successfully converted %s to %s (%s) in %v\n", inputPath, outputPath, targetCodec, timer.Elapsed())
}
