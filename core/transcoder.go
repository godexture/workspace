package godec

import (
	"context"
	"fmt"
	"os"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/core/resolver"
)

type Transcoder struct {
	resolver resolver.Bundle
	runner   pipeline.Runner
}

func (t *Transcoder) Execute(ctx context.Context, inputURI, outputURI string, targets []manifest.Capability) error {
	input, err := os.OpenFile(inputURI, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer input.Close()

	demuxerManifest, err := t.resolver.Demuxer.ResolveDemuxer(input)
	if err != nil {
		return fmt.Errorf("failed to detect demuxer: %w", err)
	}

	demuxer, err := demuxerManifest.Factory(input, nil)
	if err != nil {
		return fmt.Errorf("failed to create demuxer: %w", err)
	}

	demuxerOutPorts := demuxer.OutputPorts()
	demuxerOutPort, err := resolver.ResolveDefaultAudioPort(demuxerOutPorts)
	if err != nil {
		return fmt.Errorf("failed to resolve default audio port: %w", err)
	}

	decoderManifest, err := t.resolver.Decoder.ResolveDecoder(demuxerOutPort.StreamInfo())
	if err != nil {
		return fmt.Errorf("failed to detect decoder: %w", err)
	}

	decoder, err := decoderManifest.Factory(nil)
	if err != nil {
		return fmt.Errorf("failed to create decoder: %w", err)
	}

	pipeline.Link(demuxer, demuxerOutPort.ID(), decoder, "in")

	encoder, err := t.resolver.Encoder.ResolveEncoder("pcm", nil)
	if err != nil {
		return fmt.Errorf("failed to resolve encoder: %w", err)
	}

	return nil
}
