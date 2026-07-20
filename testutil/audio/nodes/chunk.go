package nodes

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

type audioChunkNode struct {
	in        *node.InPort[media.Frame]
	out       *node.OutPort[media.Frame]
	chunkSize int
}

func NewAudioChunk(chunkSize int) *audioChunkNode {
	return &audioChunkNode{
		in:        node.NewInPort[media.Frame]("in", nil),
		out:       node.NewOutPort[media.Frame]("out", media.StreamInfo{}),
		chunkSize: chunkSize,
	}
}

func (n *audioChunkNode) InputPorts() map[string]*node.InPort[media.Frame] {
	return map[string]*node.InPort[media.Frame]{"in": n.in}
}

func (n *audioChunkNode) OutputPorts() map[string]*node.OutPort[media.Frame] {
	return map[string]*node.OutPort[media.Frame]{"out": n.out}
}

func (n *audioChunkNode) Start(ctx context.Context) error {
	out := n.out.Edge()
	if n.in.Edge() == nil || out == nil {
		return fmt.Errorf("audio chunker ports not connected")
	}
	if n.chunkSize <= 0 {
		return fmt.Errorf("invalid audio chunk size: %d", n.chunkSize)
	}
	defer out.Close()

	var attrs media.AudioAttributes
	var configured bool
	var pending []byte
	for {
		frame, err := n.in.Pull(ctx)
		if errors.Is(err, io.EOF) {
			if len(pending) > 0 {
				return pushAudioBytes(ctx, out, pending, attrs)
			}
			return nil
		}
		if err != nil {
			return err
		}
		audioFrame, ok := frame.(*media.AudioFrame)
		if !ok {
			frame.Release()
			return fmt.Errorf("expected AudioFrame, got %T", frame)
		}
		frameAttrs := media.AudioAttributes{
			SampleRate: audioFrame.SampleRate, Format: audioFrame.Format, BitsPerSample: audioFrame.BitsPerSample, ChannelLayout: audioFrame.Layout,
		}
		if !configured {
			attrs = frameAttrs
			configured = true
		} else if frameAttrs.SampleRate != attrs.SampleRate || frameAttrs.Format != attrs.Format ||
			frameAttrs.BitsPerSample != attrs.BitsPerSample || frameAttrs.ChannelLayout != attrs.ChannelLayout {
			frame.Release()
			return fmt.Errorf("audio attributes changed while rechunking")
		}
		chunkBytes := n.chunkSize * attrs.ChannelLayout.ChannelCount() * attrs.Format.BytesPerSample()
		data := audioFrame.Planes()[0]
		if len(pending) == 0 && len(data) == chunkBytes {
			if err := out.Push(ctx, frame); err != nil {
				frame.Release()
				return err
			}
			continue
		}
		err = func() error {
			defer frame.Release()
			if len(pending) > 0 {
				take := min(chunkBytes-len(pending), len(data))
				pending = append(pending, data[:take]...)
				data = data[take:]
				if len(pending) == chunkBytes {
					if err := pushAudioBytes(ctx, out, pending, attrs); err != nil {
						return err
					}
					pending = pending[:0]
				}
			}
			for len(data) >= chunkBytes {
				if err := pushAudioBytes(ctx, out, data[:chunkBytes], attrs); err != nil {
					return err
				}
				data = data[chunkBytes:]
			}
			pending = append(pending, data...)
			return nil
		}()
		if err != nil {
			return err
		}
	}
}

func (n *audioChunkNode) Close() error { return nil }

func pushAudioBytes(ctx context.Context, out node.Edge[media.Frame], data []byte, attrs media.AudioAttributes) error {
	bytesPerFrame := attrs.ChannelLayout.ChannelCount() * attrs.Format.BytesPerSample()
	if bytesPerFrame <= 0 || len(data)%bytesPerFrame != 0 {
		return fmt.Errorf("invalid audio frame byte alignment: %d", len(data))
	}
	frame := media.NewAudioFrame(attrs.Format, attrs.ChannelLayout, attrs.SampleRate, len(data)/bytesPerFrame,
		media.WithAudioBitsPerSample(attrs.BitsPerSample))
	copy(frame.Planes()[0], data)
	var wrapped media.Frame = frame
	if err := out.Push(ctx, wrapped); err != nil {
		frame.Release()
		return err
	}
	return nil
}
