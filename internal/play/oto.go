package play

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/sdk/conversion"
)

type player interface {
	Play()
	Pause()
	IsPlaying() bool
	SetVolume(float64)
	Volume() float64
	BufferedSize() int
	Err() error
}

type outputContext interface {
	NewPlayer(io.Reader) player
	Err() error
}

type backend interface {
	Open(media.StreamInfo) (outputContext, <-chan struct{}, error)
}

type otoBackend struct{}

type otoOutputContext struct{ context *oto.Context }

func (c otoOutputContext) NewPlayer(source io.Reader) player {
	return c.context.NewPlayer(source)
}

func (c otoOutputContext) Err() error {
	return c.context.Err()
}

func (otoBackend) Open(stream media.StreamInfo) (outputContext, <-chan struct{}, error) {
	format, err := otoFormat(stream.Audio.Format)
	if err != nil {
		return nil, nil, err
	}
	context, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate: stream.Audio.SampleRate, ChannelCount: stream.Audio.ChannelCount(), Format: format,
	})
	if err != nil {
		return nil, nil, err
	}
	return otoOutputContext{context: context}, ready, nil
}

func otoFormat(format media.SampleFormat) (oto.Format, error) {
	switch format {
	case media.SampleFormatF32:
		return oto.FormatFloat32LE, nil
	case media.SampleFormatU8:
		return oto.FormatUnsignedInt8, nil
	case media.SampleFormatS16:
		return oto.FormatSignedInt16LE, nil
	default:
		return 0, fmt.Errorf("unsupported Oto sample format %q", format)
	}
}

type Controller struct {
	backend backend

	mu      sync.Mutex
	context outputContext
	ready   <-chan struct{}
	stream  media.StreamInfo
	volume  float64
	active  *sink
}

func NewController() *Controller {
	return newController(otoBackend{})
}

func newController(backend backend) *Controller {
	return &Controller{backend: backend, volume: 1}
}

func (c *Controller) Sink() conversion.PlaybackSink {
	return conversion.PlaybackSink{
		Name:         "oto",
		Requirements: otoRequirements(),
		Factory:      c.newSink,
	}
}

func otoRequirements() []manifest.Capability {
	return []manifest.Capability{&manifest.AudioConstraint{
		Codecs:      []media.CodecID{media.CodecLPCM},
		SampleRates: manifest.IntConstraint{Values: []int{44100, 48000}},
		Channels:    manifest.IntConstraint{Values: []int{1, 2}},
		SampleFormats: []manifest.SampleFormatConstraint{
			{Format: media.SampleFormatF32, BitsPerSample: manifest.IntConstraint{Values: []int{32}}},
			{Format: media.SampleFormatS16, BitsPerSample: manifest.IntConstraint{Values: []int{16}}},
			{Format: media.SampleFormatU8, BitsPerSample: manifest.IntConstraint{Values: []int{8}}},
		},
	}}
}

func (c *Controller) newSink(stream media.StreamInfo) (node.Sink, error) {
	if _, err := otoFormat(stream.Audio.Format); err != nil {
		return nil, err
	}
	if stream.Audio.SampleRate <= 0 || stream.Audio.ChannelCount() < 1 || stream.Audio.ChannelCount() > 2 {
		return nil, fmt.Errorf("invalid Oto output stream")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.context == nil {
		context, ready, err := c.backend.Open(stream)
		if err != nil {
			return nil, err
		}
		c.context, c.ready, c.stream = context, ready, stream
	} else if c.stream.Audio != stream.Audio {
		return nil, fmt.Errorf("Oto output format changed within one playback session")
	}
	reader, writer := io.Pipe()
	return &sink{controller: c, reader: reader, writer: writer}, nil
}

func (c *Controller) Toggle() (bool, error) {
	c.mu.Lock()
	if c.active == nil {
		c.mu.Unlock()
		return false, errors.New("playback has not started")
	}
	active := c.active
	if c.active.paused {
		active.paused = false
		c.mu.Unlock()
		go active.player.Play()
		return false, nil
	}
	active.paused = true
	c.mu.Unlock()
	active.player.Pause()
	return true, nil
}

func (c *Controller) AdjustVolume(delta float64) (float64, error) {
	c.mu.Lock()
	if c.active == nil {
		c.mu.Unlock()
		return 0, errors.New("playback has not started")
	}
	c.volume = min(1, max(0, c.volume+delta))
	volume, player := c.volume, c.active.player
	c.mu.Unlock()
	player.SetVolume(volume)
	return volume, nil
}

func (c *Controller) Volume() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.volume
}

func (c *Controller) activate(value *sink) {
	c.mu.Lock()
	c.active = value
	volume := c.volume
	c.mu.Unlock()
	value.player.SetVolume(volume)
}

func (c *Controller) deactivate(value *sink) {
	c.mu.Lock()
	if c.active == value {
		c.active = nil
	}
	c.mu.Unlock()
}

type sink struct {
	controller *Controller
	reader     *io.PipeReader
	writer     *io.PipeWriter
	in         *node.InPort[media.Frame]
	player     player
	paused     bool
	closeOnce  sync.Once
}

func (s *sink) InputPorts() map[string]*node.InPort[media.Frame] {
	if s.in == nil {
		s.in = node.NewInPort[media.Frame]("in")
	}
	return map[string]*node.InPort[media.Frame]{"in": s.in}
}

func (s *sink) Start(ctx context.Context) error {
	if s.in == nil || s.in.Edge() == nil {
		return errors.New("playback sink input is not connected")
	}
	s.controller.mu.Lock()
	output, ready := s.controller.context, s.controller.ready
	s.controller.mu.Unlock()
	if output == nil {
		return errors.New("playback context is not initialized")
	}
	select {
	case <-ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := output.Err(); err != nil {
		return err
	}
	s.player = output.NewPlayer(s.reader)
	s.controller.activate(s)
	defer s.controller.deactivate(s)
	stopCancellation := make(chan struct{})
	defer close(stopCancellation)
	go func() {
		select {
		case <-ctx.Done():
			s.close()
		case <-stopCancellation:
		}
	}()
	go s.player.Play()

	for {
		frame, err := s.in.Pull(ctx)
		if errors.Is(err, io.EOF) {
			if closeErr := s.writer.Close(); closeErr != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return closeErr
			}
			return s.waitForDrain(ctx)
		}
		if err != nil {
			s.close()
			return err
		}
		writeErr := s.writeFrame(frame)
		frame.Release()
		if writeErr != nil {
			s.close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return writeErr
		}
	}
}

func (s *sink) writeFrame(frame media.Frame) error {
	audio, ok := frame.(*media.AudioFrame)
	if !ok {
		return fmt.Errorf("playback sink received %T, want audio frame", frame)
	}
	if !audio.Format.IsPacked() {
		return fmt.Errorf("playback sink requires packed audio")
	}
	data := audio.Planes()[0]
	for len(data) > 0 {
		n, err := s.writer.Write(data)
		data = data[n:]
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *sink) waitForDrain(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			s.close()
			return err
		}
		if err := s.player.Err(); err != nil {
			return err
		}
		if !s.isPaused() && !s.player.IsPlaying() && s.player.BufferedSize() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			s.close()
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *sink) isPaused() bool {
	s.controller.mu.Lock()
	defer s.controller.mu.Unlock()
	return s.controller.active == s && s.paused
}

func (s *sink) Close() error {
	s.close()
	return nil
}

func (s *sink) close() {
	s.closeOnce.Do(func() {
		if s.player != nil {
			s.player.Pause()
		}
		_ = s.writer.CloseWithError(io.ErrClosedPipe)
		_ = s.reader.CloseWithError(io.ErrClosedPipe)
	})
}
