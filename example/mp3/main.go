package main

import (
	"context"
	"fmt"
	"os"

	mp3Codec "github.com/godexture/codec-mp3"
	pcm "github.com/godexture/codec-pcm"
	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/core/resolver"
	mp3Format "github.com/godexture/format-mp3"
	wavFormat "github.com/godexture/format-wav"
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

	// Resolve Demuxer Node from registry
	demuxResolver := resolver.NewDefaultDemuxerResolver(godec.DefaultRegistry.Demuxers)
	demuxManifest, err := demuxResolver.ResolveDemuxer(file)
	if err != nil {
		fmt.Printf("Failed to resolve demuxer: %v\n", err)
		return
	}
	demuxNode, err := demuxManifest.Factory(file, mp3Format.Config{})
	if err != nil {
		fmt.Printf("Failed to create demuxer node: %v\n", err)
		return
	}

	streams, err := demuxNode.Streams()
	if err != nil {
		fmt.Printf("Failed to analyze input: %v\n", err)
		return
	}

	if len(streams) == 0 {
		fmt.Println("No audio streams found")
		return
	}

	// Resolve Decoder Node from registry
	decResolver := resolver.NewDefaultDecoderResolver(godec.DefaultRegistry.Decoders)
	decManifest, err := decResolver.ResolveDecoder(streams[0])
	if err != nil {
		fmt.Printf("Failed to resolve decoder: %v\n", err)
		return
	}
	decNode, err := decManifest.Factory(mp3Codec.DecoderConfig{})
	if err != nil {
		fmt.Printf("Failed to create decoder node: %v\n", err)
		return
	}

	// Resolve Encoder Node from registry
	encResolver := resolver.NewDefaultEncoderResolver(godec.DefaultRegistry.Encoders)
	encManifest, err := encResolver.ResolveEncoder(media.CodecLPCM)
	if err != nil {
		fmt.Printf("Failed to resolve encoder: %v\n", err)
		return
	}
	encNode, err := encManifest.Factory(pcm.EncoderConfig{CodecID: media.CodecLPCM})
	if err != nil {
		fmt.Printf("Failed to create encoder node: %v\n", err)
		return
	}

	outputFile, err := os.Create(outputPath)
	if err != nil {
		fmt.Printf("Failed to create output: %v\n", err)
		return
	}
	defer outputFile.Close()

	// Resolve Muxer Node from registry
	muxResolver := resolver.NewDefaultMuxerResolver(godec.DefaultRegistry.Muxers)
	muxManifest, err := muxResolver.ResolveMuxer(wavFormat.Config{})
	if err != nil {
		fmt.Printf("Failed to resolve muxer: %v\n", err)
		return
	}
	muxNode, err := muxManifest.Factory(outputFile, wavFormat.Config{})
	if err != nil {
		fmt.Printf("Failed to create muxer node: %v\n", err)
		return
	}

	outputStream := streams[0]
	outputStream.Codec = media.CodecLPCM
	outputStream.Audio.CodecID = media.CodecLPCM
	outputStream.Audio.Format = media.SampleFormatF32

	if _, err := muxNode.AddStream(outputStream); err != nil {
		fmt.Printf("Failed to add stream to muxer: %v\n", err)
		return
	}

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
