package play

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/pipeline"
)

func TestSinkStreamsWhilePlayerPlayReadsSynchronously(t *testing.T) {
	sink, output := newTestSink(t)
	edge := pipeline.NewChanEdge[media.Frame](1)
	sink.InputPorts()["in"].Connect(edge)
	frame := media.NewAudioFrame(media.SampleFormatS16, media.LayoutStereo2_0, 44100, 2048)
	if err := edge.Push(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	edge.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sink.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if output.player.bytes == 0 {
		t.Fatal("player read no PCM bytes")
	}
}

func TestSinkCancellationInterruptsPlayerRead(t *testing.T) {
	sink, output := newTestSink(t)
	edge := pipeline.NewChanEdge[media.Frame](0)
	sink.InputPorts()["in"].Connect(edge)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)

	if err := sink.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want context.Canceled", err)
	}
	select {
	case <-output.player.finished:
	case <-time.After(time.Second):
		t.Fatal("player read remained blocked after cancellation")
	}
}

func TestResumeDoesNotBlockControls(t *testing.T) {
	reader, writer := io.Pipe()
	player := newReaderPlayer(reader)
	controller := &Controller{active: &sink{player: player, paused: true}}
	done := make(chan error, 1)
	go func() {
		_, err := controller.Toggle()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Toggle() error = %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Toggle() blocked in Player.Play")
	}
	_ = writer.Close()
	<-player.finished
}

func newTestSink(t *testing.T) (*sink, *readerContext) {
	t.Helper()
	output := &readerContext{}
	controller := newController(readerBackend{output: output})
	created, err := controller.newSink(media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecLPCM,
			Audio: media.AudioAttributes{
				SampleRate: 44100, Format: media.SampleFormatS16,
				BitsPerSample: 16, ChannelLayout: media.LayoutStereo2_0,
			},
		},
	})
	if err != nil {
		t.Fatalf("newSink() error = %v", err)
	}
	return created.(*sink), output
}

type readerBackend struct{ output *readerContext }

func (b readerBackend) Open(media.StreamInfo) (outputContext, <-chan struct{}, error) {
	ready := make(chan struct{})
	close(ready)
	return b.output, ready, nil
}

type readerContext struct{ player *readerPlayer }

func (c *readerContext) NewPlayer(source io.Reader) player {
	c.player = newReaderPlayer(source)
	return c.player
}

func (*readerContext) Err() error { return nil }

type readerPlayer struct {
	source   io.Reader
	finished chan struct{}
	once     sync.Once
	mu       sync.Mutex
	playing  bool
	volume   float64
	bytes    int64
	err      error
}

func newReaderPlayer(source io.Reader) *readerPlayer {
	return &readerPlayer{source: source, finished: make(chan struct{})}
}

func (p *readerPlayer) Play() {
	p.mu.Lock()
	if p.playing {
		p.mu.Unlock()
		return
	}
	p.playing = true
	p.mu.Unlock()
	n, err := io.Copy(io.Discard, p.source)
	p.mu.Lock()
	p.bytes += n
	p.playing = false
	if err != nil {
		p.err = err
	}
	p.mu.Unlock()
	p.once.Do(func() { close(p.finished) })
}

func (p *readerPlayer) Pause() {
	p.mu.Lock()
	p.playing = false
	p.mu.Unlock()
}

func (p *readerPlayer) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

func (p *readerPlayer) SetVolume(volume float64) {
	p.mu.Lock()
	p.volume = volume
	p.mu.Unlock()
}

func (p *readerPlayer) Volume() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

func (*readerPlayer) BufferedSize() int { return 0 }

func (p *readerPlayer) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

var _ node.Sink = (*sink)(nil)
