package main

import (
	"context"
	"fmt"
	"os"

	mp3Codec "github.com/godexture/codec-mp3"
	pcm "github.com/godexture/codec-pcm"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/pipeline"
	mp3Format "github.com/godexture/format-mp3"
	wavFormat "github.com/godexture/format-wav"
	"github.com/godexture/sdk/engine"
)

func main() {
	inputPath := "assets/mpeg3.mp3"
	outputPath := "out_mp3.wav"

	if len(os.Args) >= 2 {
		inputPath = os.Args[1]
	}
	if len(os.Args) >= 3 {
		outputPath = os.Args[2]
	}

	fmt.Printf("Converting %s to %s\n", inputPath, outputPath)

	file, err := os.Open(inputPath)
	if err != nil {
		fmt.Printf("Failed to open input: %v\n", err)
		return
	}
	defer file.Close()

	demuxer, err := mp3Format.NewDemuxerEngine(file)
	if err != nil {
		fmt.Printf("Failed to create mp3 demuxer: %v\n", err)
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

	decoder := mp3Codec.NewDecoderEngine(mp3Codec.DecoderConfig{})
	encoder := pcm.NewEncoderEngine(pcm.EncoderConfig{CodecID: media.CodecLPCM})

	outputFile, err := os.Create(outputPath)
	if err != nil {
		fmt.Printf("Failed to create output: %v\n", err)
		return
	}
	defer outputFile.Close()

	muxer := wavFormat.NewMuxerEngine(outputFile)

	outputStream := streams[0]
	outputStream.Codec = media.CodecLPCM
	outputStream.Audio.CodecID = media.CodecLPCM
	outputStream.Audio.Format = media.SampleFormatF32

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

	fmt.Printf("Successfully converted %s to %s\n", inputPath, outputPath)
}
