package nodes

import (
	"context"
	"errors"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/sdk/testutil/audio/pcm"
)

const pcmFramesPerChunk = 4096

func pushFrame(ctx context.Context, edge node.Edge[media.Frame], frame media.Frame) error {
	if err := edge.Push(ctx, frame); err != nil {
		frame.Release()
		return err
	}
	return nil
}

func retainAndPushFrame(ctx context.Context, edge node.Edge[media.Frame], frame media.Frame) error {
	frame.Retain()
	return pushFrame(ctx, edge, frame)
}

func consumeUntilEOF[T media.Retainer](ctx context.Context, port *node.InPort[T], process func(T) error) error {
	for {
		val, err := port.Pull(ctx)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		err = process(val)
		val.Release()
		if err != nil {
			return err
		}
	}
}

// sampleStream is a helper for FrameCompareNode that simplifies pulling and buffering audio samples.
type sampleStream struct {
	port    *node.InPort[media.Frame]
	samples []float32
	pos     int
	eof     bool
	count   int64
}

func (s *sampleStream) fill(ctx context.Context) error {
	if s.pos < len(s.samples) || s.eof {
		return nil
	}
	frame, err := s.port.Pull(ctx)
	if errors.Is(err, io.EOF) {
		s.eof = true
		s.samples = s.samples[:0]
		return nil
	}
	if err != nil {
		return err
	}
	defer frame.Release()
	audioFrame, ok := frame.(*media.AudioFrame)
	if !ok {
		return errors.New("expected AudioFrame")
	}
	s.samples, err = pcm.ConvertToFloat32(s.samples, audioFrame)
	if err != nil {
		return err
	}
	s.pos = 0
	s.count += int64(len(s.samples))
	return nil
}

func (s *sampleStream) next() []float32 {
	if s.pos >= len(s.samples) {
		return nil
	}
	return s.samples[s.pos:]
}

func (s *sampleStream) advance(n int) {
	s.pos += n
}
