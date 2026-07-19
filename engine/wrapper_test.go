package engine

import (
	"context"
	"testing"
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/pipeline"
)

// fakeEncoderEngine verifies that adapter Close owns engine teardown and is
// idempotent independently from Start's exit path.
type fakeEncoderEngine struct {
	sendErr    error
	flushErr   error
	flushed    bool
	closed     bool
	closeCount int
	ready      chan struct{}
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
	f.closeCount++
	return nil
}

func (f *fakeEncoderEngine) OutputReady() <-chan struct{} {
	return f.ready
}

func connectEncoderAdapter(t *testing.T, engine EncoderEngine) (node interface {
	Start(ctx context.Context) error
	Close() error
}, in *pipeline.ChanEdge[media.Frame]) {
	t.Helper()
	adapter := WrapEncoder(engine)
	in = pipeline.NewChanEdge[media.Frame](1)
	out := pipeline.NewChanEdge[*media.Packet](1)
	adapter.InputPorts()["in"].Connect(in)
	adapter.OutputPorts()["out"].Connect(out)
	return adapter, in
}

func TestEncoderAdapter_CloseAfterGracefulCompletion(t *testing.T) {
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
	if fake.closed {
		t.Fatal("engine closed before adapter ownership was released")
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !fake.closed {
		t.Fatal("engine was not closed")
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if fake.closeCount != 1 {
		t.Fatalf("engine Close() called %d times, want 1", fake.closeCount)
	}
}

func TestEncoderAdapter_CloseAfterSendError(t *testing.T) {
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
	if fake.closed {
		t.Fatal("engine closed before adapter ownership was released")
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !fake.closed {
		t.Fatal("engine was not closed")
	}
}

func TestEncoderAdapter_CloseAfterContextCancellation(t *testing.T) {
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
	if fake.closed {
		t.Fatal("engine closed before adapter ownership was released")
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !fake.closed {
		t.Fatal("engine was not closed")
	}
}
