package conversion_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/sdk/conversion"
)

func TestBuildPlaybackRunsWAV(t *testing.T) {
	var wav bytes.Buffer
	writeTestWAV(t, &wav)

	recorder := &frameRecorder{}
	built, err := conversion.BuildPlayback(context.Background(), conversion.InputSet{Main: bytes.NewReader(wav.Bytes())}, conversion.PlaybackSpec{}, conversion.PlaybackSink{
		Name: "recorder",
		Requirements: []manifest.Capability{&manifest.AudioConstraint{
			Codecs: []media.CodecID{media.CodecLPCM},
		}},
		Factory: func(media.StreamInfo) (node.Sink, error) { return recorder, nil },
	}, pipeline.ObservationOff)
	if err != nil {
		t.Fatalf("BuildPlayback() error = %v", err)
	}
	defer built.Close()
	if err := built.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if recorder.frames == 0 || recorder.bytes == 0 {
		t.Fatalf("recorder captured frames=%d bytes=%d, want non-zero", recorder.frames, recorder.bytes)
	}
}

type frameRecorder struct {
	in     *node.InPort[media.Frame]
	frames int
	bytes  int
}

func (r *frameRecorder) InputPorts() map[string]*node.InPort[media.Frame] {
	if r.in == nil {
		r.in = node.NewInPort[media.Frame]("in")
	}
	return map[string]*node.InPort[media.Frame]{"in": r.in}
}

func (r *frameRecorder) Start(ctx context.Context) error {
	for {
		frame, err := r.in.Pull(ctx)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, plane := range frame.(*media.AudioFrame).Planes() {
			r.bytes += len(plane)
		}
		r.frames++
		frame.Release()
	}
}

func (*frameRecorder) Close() error { return nil }
