package main

import (
	"context"
	"fmt"
	"os"

	pcm "github.com/godexture/codec-pcm"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/pipeline"
	wav "github.com/godexture/format-wav"
	"github.com/godexture/sdk/engine"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run example/pcm.go <input.wav> <output.wav> [pcma|pcmu]")
		return
	}

	inputPath := os.Args[1]
	outputPath := os.Args[2]
	codecType := "pcmu"
	if len(os.Args) > 3 {
		codecType = os.Args[3]
	}

	targetCodec := media.CodecPCMU
	if codecType == "pcma" {
		targetCodec = media.CodecPCMA
	}

	file, err := os.Open(inputPath)
	if err != nil {
		fmt.Printf("Failed to open input: %v\n", err)
		return
	}
	defer file.Close()

	demuxer, err := wav.NewDemuxerEngine(file)
	if err != nil {
		fmt.Printf("Failed to create demuxer: %v\n", err)
		return
	}
	streams, _, err := demuxer.Analyze()
	if err != nil {
		fmt.Printf("Failed to analyze input: %v\n", err)
		return
	}

	if len(streams) == 0 {
		fmt.Println("No audio streams found")
		return
	}

	audio := streams[0].MediaAttributes.Audio
	decoder := pcm.NewDecoderEngine(pcm.NewConfigWithAudio(audio.SampleRate, audio.Format, audio.ChannelLayout))
	encoder := pcm.NewEncoderEngine(pcm.EncoderConfig{CodecID: targetCodec})

	outputFile, err := os.Create(outputPath)
	if err != nil {
		fmt.Printf("Failed to create output: %v\n", err)
		return
	}
	defer outputFile.Close()

	muxer := wav.NewMuxerEngine(outputFile)
	outputStream := streams[0]
	outputStream.Codec = targetCodec
	outputStream.Audio.CodecID = targetCodec
	outputStream.Audio.Format = media.SampleFormatU8

	if _, err := muxer.AddStream(outputStream); err != nil {
		fmt.Printf("Failed to add stream to muxer: %v\n", err)
		return
	}

	demuxNode := engine.WrapDemuxer(demuxer)
	decNode := engine.WrapDecoder(decoder)
	encNode := engine.WrapEncoder(encoder)
	muxNode := engine.WrapMuxer(muxer)

	if err := pipeline.Link(demuxNode, "out", decNode, "in"); err != nil {
		fmt.Printf("Failed to link demuxer and decoder: %v\n", err)
		return
	}
	if err := pipeline.Link(decNode, "out", encNode, "in"); err != nil {
		fmt.Printf("Failed to link decoder and encoder: %v\n", err)
		return
	}
	if err := pipeline.Link(encNode, "out", muxNode, "in"); err != nil {
		fmt.Printf("Failed to link encoder and muxer: %v\n", err)
		return
	}

	runner := pipeline.NewRunner()
	nodes := []node.Node{demuxNode, decNode, encNode, muxNode}
	if err := runner.Run(context.Background(), nodes); err != nil {
		fmt.Printf("Pipeline execution failed: %v\n", err)
		return
	}

	fmt.Printf("Successfully converted %s to %s (%s)\n", inputPath, outputPath, codecType)
}
