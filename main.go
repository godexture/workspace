package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"

	"github.com/godexture/filter-audio"
	"github.com/godexture/sdk/dsp"
	"github.com/godexture/sdk/profiling"
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
	debug.SetGCPercent(200)

	fmt.Printf("Environment: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("SIMD Support: AVX2=%v, AVX2+FMA=%v\n", dsp.HasAVX2, dsp.HasAVX2FMA)

	if len(os.Args) <= 2 {
		fmt.Println("Usage: go run . <input> <output.wav>")
		return
	}

	inputPath := os.Args[1]
	outputPath := os.Args[2]

	if profilePath := os.Getenv("GODEC_PPROF"); profilePath != "" {
		if err := profiling.RejectPathCollision(profilePath, inputPath, outputPath); err != nil {
			fmt.Printf("Failed to validate profile path: %v\n", err)
			return
		}
		profileFile, err := os.Create(profilePath)
		if err != nil {
			fmt.Printf("Failed to create profile: %v\n", err)
			return
		}
		defer profileFile.Close()
		if err := pprof.StartCPUProfile(profileFile); err != nil {
			fmt.Printf("Failed to start CPU profile: %v\n", err)
			return
		}
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
		Filters: []routing.FilterSpec{
			{
				Config: filter.MustNewResampleConfig(
					filter.WithSampleRate(48000),
				),
			},
		},
		MuxConfig: flacFormat.MustNewMuxerConfig(),
	}

	geometry, err := negotiator.NegotiateConversion(context.Background(), spec)
	if err != nil {
		fmt.Printf("Failed to negotiate conversion: %v\n", err)
		return
	}

	// 2. Build Pipeline
	builder := godec.NewBuilder()
	conversion, err := builder.Build(geometry)
	if err != nil {
		fmt.Printf("Failed to build pipeline: %v\n", err)
		return
	}
	defer conversion.Close()

	fmt.Printf("Starting conversion from %s to %s (%s)\n", inputPath, outputPath, targetCodec)
	timer := timer.New()

	if err := conversion.Run(context.Background()); err != nil {
		fmt.Printf("Pipeline execution failed: %v\n", err)
		return
	}

	defer fmt.Printf("Successfully converted %s to %s (%s) in %v\n", inputPath, outputPath, targetCodec, timer.Elapsed())
}
