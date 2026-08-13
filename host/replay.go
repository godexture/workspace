package host

import (
	"context"
	"errors"
	"sync"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/media/buffer"
)

type replaySession struct {
	underlying access.Session
	reader     access.Sequential
	chunks     []buffer.Handle
	chunk      int
	offset     int

	mu       sync.Mutex
	close    sync.Once
	closeErr error
}

func (s *probeStore) ReplaySession(session access.Session) (access.Session, error) {
	if s == nil || s.sequential == nil || s.random != nil || session == nil {
		return nil, errors.New("prefix replay requires one sequential-only probe session")
	}
	reader, ok := session.(access.Sequential)
	if !ok {
		return nil, access.ErrCapabilityView
	}
	chunks := append([]buffer.Handle(nil), s.handles...)
	total := int64(0)
	for _, chunk := range chunks {
		if !chunk.Valid() {
			return nil, errors.New("prefix replay contains an invalid probe handle")
		}
		total += int64(chunk.Bytes().Len())
	}
	if total != s.offset {
		return nil, errors.New("prefix replay does not cover the consumed sequential range")
	}
	s.handles = nil
	s.views = nil
	return &replaySession{underlying: session, reader: reader, chunks: chunks}, nil
}

func (s *replaySession) Capabilities() access.Capabilities {
	if s == nil || s.underlying == nil {
		return access.Capabilities{}
	}
	return s.underlying.Capabilities()
}

func (s *replaySession) Read(ctx context.Context, destination []byte) (int, error) {
	if s == nil || s.reader == nil {
		return 0, errors.New("prefix replay session is closed")
	}
	if cause := context.Cause(ctx); cause != nil {
		return 0, cause
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	written := 0
	for written < len(destination) && s.chunk < len(s.chunks) {
		value := s.chunks[s.chunk].Bytes()
		remaining, err := value.From(s.offset)
		if err != nil {
			return written, err
		}
		count := remaining.CopyTo(destination[written:])
		written += count
		s.offset += count
		if s.offset == value.Len() {
			s.chunks[s.chunk].Release()
			s.chunks[s.chunk] = buffer.Handle{}
			s.chunk++
			s.offset = 0
		}
	}
	if written != 0 || len(destination) == 0 {
		return written, nil
	}
	return s.reader.Read(ctx, destination)
}

func (s *replaySession) Close() error {
	if s == nil {
		return nil
	}
	s.close.Do(func() {
		s.mu.Lock()
		for index := s.chunk; index < len(s.chunks); index++ {
			s.chunks[index].Release()
			s.chunks[index] = buffer.Handle{}
		}
		s.reader = nil
		s.mu.Unlock()
		if s.underlying != nil {
			s.closeErr = s.underlying.Close()
			s.underlying = nil
		}
	})
	return s.closeErr
}
