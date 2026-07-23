package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
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
	sent       chan struct{}
}

type trackedFrame struct {
	releases atomic.Int32
}

func (*trackedFrame) Retain()        {}
func (f *trackedFrame) Release()     { f.releases.Add(1) }
func (*trackedFrame) Pts() media.Pts { return 0 }

type fakeMultiFilterEngine struct {
	mu       sync.Mutex
	received map[string]int
	ended    map[string]int
	flushed  bool
}

func (f *fakeMultiFilterEngine) SendFrame(*media.Frame) error {
	f.mu.Lock()
	f.received["in"]++
	f.mu.Unlock()
	return nil
}

func (f *fakeMultiFilterEngine) SendInput(port string, _ *media.Frame) error {
	f.mu.Lock()
	f.received[port]++
	f.mu.Unlock()
	return nil
}

func (*fakeMultiFilterEngine) ReceiveFrame() (*media.Frame, error) { return nil, ErrEAGAIN }

func (f *fakeMultiFilterEngine) EndInput(port string) error {
	f.mu.Lock()
	f.ended[port]++
	f.mu.Unlock()
	return nil
}

func (f *fakeMultiFilterEngine) Flush() error {
	f.mu.Lock()
	f.flushed = true
	f.mu.Unlock()
	return nil
}

func (f *fakeEncoderEngine) SendFrame(frame *media.Frame) error {
	if f.sent != nil {
		close(f.sent)
	}
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

	frame := &trackedFrame{}
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
	if got := frame.releases.Load(); got != 1 {
		t.Fatalf("input frame released %d times, want 1", got)
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
	fake := &fakeEncoderEngine{ready: make(chan struct{}), sent: make(chan struct{})} // output never becomes ready
	adapter, in := connectEncoderAdapter(t, fake)
	frame := &trackedFrame{}
	var wrapped media.Frame = frame
	if err := in.Push(context.Background(), wrapped); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- adapter.Start(ctx) }()

	<-fake.sent
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
	if got := frame.releases.Load(); got != 1 {
		t.Fatalf("input frame released %d times, want 1", got)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !fake.closed {
		t.Fatal("engine was not closed")
	}
}

func TestMultiFilterAdapterSchedulesMultipleRunInputs(t *testing.T) {
	t.Parallel()
	engine := &fakeMultiFilterEngine{received: make(map[string]int), ended: make(map[string]int)}
	adapter := WrapMultiFilter(engine,
		FilterInput{ID: "in", Phase: node.InputPhaseRun},
		FilterInput{ID: "sidechain", Phase: node.InputPhaseRun},
	)
	main := pipeline.NewChanEdge[media.Frame](1)
	sidechain := pipeline.NewChanEdge[media.Frame](1)
	out := pipeline.NewChanEdge[media.Frame](1)
	adapter.InputPorts()["in"].Connect(main)
	adapter.InputPorts()["sidechain"].Connect(sidechain)
	adapter.OutputPorts()["out"].Connect(out)

	mainFrame := &trackedFrame{}
	sidechainFrame := &trackedFrame{}
	if err := main.Push(context.Background(), mainFrame); err != nil {
		t.Fatal(err)
	}
	if err := sidechain.Push(context.Background(), sidechainFrame); err != nil {
		t.Fatal(err)
	}
	main.Close()
	sidechain.Close()

	if err := adapter.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	engine.mu.Lock()
	received := map[string]int{"in": engine.received["in"], "sidechain": engine.received["sidechain"]}
	ended := map[string]int{"in": engine.ended["in"], "sidechain": engine.ended["sidechain"]}
	flushed := engine.flushed
	engine.mu.Unlock()
	if received["in"] != 1 || received["sidechain"] != 1 {
		t.Fatalf("received = %#v, want one frame per port", received)
	}
	if ended["in"] != 1 || ended["sidechain"] != 1 {
		t.Fatalf("ended = %#v, want one EOF per port", ended)
	}
	if !flushed {
		t.Fatal("Flush() was not called after all run inputs ended")
	}
	if mainFrame.releases.Load() != 1 || sidechainFrame.releases.Load() != 1 {
		t.Fatalf("frame releases = (%d, %d), want (1, 1)", mainFrame.releases.Load(), sidechainFrame.releases.Load())
	}
}
