package main

import (
	"bytes"
	"context"
	"fmt"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/routing"

	_ "github.com/godexture/codec-flac"
	_ "github.com/godexture/codec-mp3"
	_ "github.com/godexture/codec-pcm"
	flacFormat "github.com/godexture/format-flac"
	mp3Format "github.com/godexture/format-mp3"
	wavFormat "github.com/godexture/format-wav"
)

// Convert takes an input byte array of media data and converts it to the specified format.
func Convert(input []byte, format string) ([]byte, error) {
	var targetCodec media.CodecID
	var muxConfig interface{}

	switch format {
	case "flac":
		targetCodec = media.CodecFLAC
		muxConfig = flacFormat.NewMuxerConfig()
	case "mp3":
		targetCodec = media.CodecMP3
		muxConfig = mp3Format.NewMuxerConfig()
	case "wav":
		targetCodec = media.CodecLPCM
		muxConfig = wavFormat.NewMuxerConfig()
	default:
		return nil, fmt.Errorf("unsupported target format: %s", format)
	}

	reader := bytes.NewReader(input)
	var writer bytes.Buffer

	negotiator := godec.NewNegotiator()
	spec := routing.ConversionSpec{
		Input:       reader,
		Output:      &writer,
		TargetCodec: targetCodec,
		MuxConfig:   muxConfig,
	}

	geometry, err := negotiator.NegotiateConversion(context.Background(), spec)
	if err != nil {
		return nil, fmt.Errorf("failed to negotiate conversion: %w", err)
	}

	builder := godec.NewBuilder()
	conversion, err := builder.Build(geometry)
	if err != nil {
		return nil, fmt.Errorf("failed to build pipeline: %w", err)
	}
	defer conversion.Close()

	if err := conversion.Run(context.Background()); err != nil {
		return nil, fmt.Errorf("pipeline execution failed: %w", err)
	}

	return writer.Bytes(), nil
}

func main() {
	select {}
}
