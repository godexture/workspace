package nodes

import (
	"context"
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/sdk/testutil/audio/pcm"
)

type slicePCMSourceNode struct {
	out   *node.OutPort[media.Frame]
	pcm   []float32
	attrs media.AudioAttributes
}

func NewSlicePCMSource(pcmData []float32, attrs media.AudioAttributes) *slicePCMSourceNode {
	return &slicePCMSourceNode{
		out:   node.NewOutPort[media.Frame]("out", media.StreamInfo{Type: media.MediaAudio, MediaAttributes: media.MediaAttributes{Audio: attrs}}),
		pcm:   pcmData,
		attrs: attrs,
	}
}

func (n *slicePCMSourceNode) OutputPorts() map[string]*node.OutPort[media.Frame] {
	return map[string]*node.OutPort[media.Frame]{"out": n.out}
}

func (n *slicePCMSourceNode) Start(ctx context.Context) error {
	out := n.out.Edge()
	if out == nil {
		return fmt.Errorf("PCM source output not connected")
	}
	defer out.Close()
	channels := n.attrs.ChannelLayout.ChannelCount()
	if channels <= 0 || len(n.pcm)%channels != 0 {
		return fmt.Errorf("invalid PCM source channel alignment")
	}
	chunkSamples := pcmFramesPerChunk * channels
	for offset := 0; offset < len(n.pcm); offset += chunkSamples {
		end := min(offset+chunkSamples, len(n.pcm))
		frame, err := pcm.CreateAudioFrame(n.pcm[offset:end], n.attrs)
		if err != nil {
			return err
		}
		if err := out.Push(ctx, *frame); err != nil {
			return err
		}
	}
	return nil
}

func (n *slicePCMSourceNode) Close() error { return nil }
