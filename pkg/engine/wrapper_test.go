package engine

import (
	"context"
	"testing"
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/pipeline"
)

// fakeEncoderEngine is a minimal EncoderEngine used to verify that
// EncoderAdapter.Start invokes Close() (via the optional engineCloser
// interface) on every exit path, not just the graceful io.EOF path that
// Flush() alone covers.
type fakeEncoderEngine struct {
	sendErr  error
	flushErr error
	flushed  bool
	closed   bool
	ready    chan struct{}
}

func (f *fakeEncoderEngine) SendFrame(frame *media.Frame) error {
	return f.sendErr
}

func (f *fakeEncoderEngine) ReceivePacket() (*media.Packet, error) {
	if f.flushed {
		return nil, ErrEOF
	}
	return nil, ErrEAGAIN
}

func (f *fakeEncoderEngine) Flush() error {
	f.flushed = true
	return f.flushErr
}

func (f *fakeEncoderEngine) Close() error {
	f.closed = true
	return nil
}

func (f *fakeEncoderEngine) OutputReady() <-chan struct{} {
	return f.ready
}

func connectEncoderAdapter(t *testing.T, engine EncoderEngine) (node interface {
	Start(ctx context.Context) error
}, in *pipeline.ChanEdge[media.Frame]) {
	t.Helper()
	adapter := WrapEncoder(engine)
	in = pipeline.NewChanEdge[media.Frame](1)
	out := pipeline.NewChanEdge[*media.Packet](1)
	adapter.InputPorts()["in"].Connect(in)
	adapter.OutputPorts()["out"].Connect(out)
	return adapter, in
}

func TestEncoderAdapter_CloseOnGracefulCompletion(t *testing.T) {
	t.Parallel()
	fake := &fakeEncoderEngine{}
	adapter, in := connectEncoderAdapter(t, fake)
	in.Close() // no frames: Pull immediately reports io.EOF

	if err := adapter.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !fake.flushed {
		t.Fatal("expected Flush() to have run on graceful completion")
	}
	if !fake.closed {
		t.Fatal("expected Close() to have run on graceful completion")
	}
}

func TestEncoderAdapter_CloseOnSendError(t *testing.T) {
	t.Parallel()
	wantErr := ErrEAGAIN // reuse a sentinel as a stand-in send failure
	fake := &fakeEncoderEngine{sendErr: wantErr}
	adapter, in := connectEncoderAdapter(t, fake)

	frame := media.NewAudioFrame(media.SampleFormatS16, media.LayoutMono1, 44100, 1)
	var wrapped media.Frame = frame
	if err := in.Push(context.Background(), wrapped); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	err := adapter.Start(context.Background())
	if err != wantErr {
		t.Fatalf("Start() error = %v, want %v", err, wantErr)
	}
	if fake.flushed {
		t.Fatal("expected Flush() NOT to have run on a send error")
	}
	if !fake.closed {
		t.Fatal("expected Close() to have run even though the send errored")
	}
}

func TestEncoderAdapter_CloseOnContextCancellation(t *testing.T) {
	t.Parallel()
	fake := &fakeEncoderEngine{ready: make(chan struct{})} // never becomes ready
	adapter, _ := connectEncoderAdapter(t, fake)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- adapter.Start(ctx) }()

	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("Start() error = %v, want %v", err, context.Canceled)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after context cancellation")
	}
	if fake.flushed {
		t.Fatal("expected Flush() NOT to have run on context cancellation")
	}
	if !fake.closed {
		t.Fatal("expected Close() to have run on context cancellation")
	}
}
