package nodes

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/godexture/godec/core/domain/media"
)

type frameEdgeStub struct {
	pushErr error
	pushed  media.Frame
}

func (e *frameEdgeStub) Push(_ context.Context, frame media.Frame) error {
	e.pushed = frame
	return e.pushErr
}

func (*frameEdgeStub) Pull(context.Context) (media.Frame, error) {
	return nil, io.EOF
}

func (*frameEdgeStub) Close() {}

type frameReferenceStub struct {
	retained int
	released int
}

func (f *frameReferenceStub) Retain()      { f.retained++ }
func (f *frameReferenceStub) Release()     { f.released++ }
func (*frameReferenceStub) Pts() media.Pts { return 0 }

func TestPushFrameBalancesFailedTransfer(t *testing.T) {
	pushErr := errors.New("push failed")
	edge := &frameEdgeStub{pushErr: pushErr}
	frame := &frameReferenceStub{}

	if err := pushFrame(t.Context(), edge, frame); !errors.Is(err, pushErr) {
		t.Fatalf("pushFrame error = %v, want %v", err, pushErr)
	}
	if edge.pushed != frame || frame.retained != 0 || frame.released != 1 {
		t.Fatalf("pushFrame references = retained %d, released %d", frame.retained, frame.released)
	}
}

func TestRetainAndPushFrameBalancesFailedTransfer(t *testing.T) {
	pushErr := errors.New("push failed")
	edge := &frameEdgeStub{pushErr: pushErr}
	frame := &frameReferenceStub{}

	if err := retainAndPushFrame(t.Context(), edge, frame); !errors.Is(err, pushErr) {
		t.Fatalf("retainAndPushFrame error = %v, want %v", err, pushErr)
	}
	if edge.pushed != frame || frame.retained != 1 || frame.released != 1 {
		t.Fatalf("retainAndPushFrame references = retained %d, released %d", frame.retained, frame.released)
	}
}
