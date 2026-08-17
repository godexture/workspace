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
	if s == nil {
		return access.Capabilities{}
	}
	s.mu.Lock()
	underlying := s.underlying
	s.mu.Unlock()
	if underlying == nil {
		return access.Capabilities{}
	}
	return underlying.Capabilities()
}

func (s *replaySession) Read(ctx context.Context, destination []byte) (int, error) {
	if s == nil {
		return 0, errors.New("prefix replay session is closed")
	}
	if cause := context.Cause(ctx); cause != nil {
		return 0, cause
	}
	s.mu.Lock()
	if s.reader == nil {
		s.mu.Unlock()
		return 0, errors.New("prefix replay session is closed")
	}
	written := 0
	for written < len(destination) && s.chunk < len(s.chunks) {
		value := s.chunks[s.chunk].Bytes()
		remaining, err := value.From(s.offset)
		if err != nil {
			s.mu.Unlock()
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
	reader := s.reader
	s.mu.Unlock()
	if written != 0 || len(destination) == 0 {
		return written, nil
	}
	return reader.Read(ctx, destination)
}

// Size delegates the selected StableSize view to the source session. Prefix
// replay changes only how already-read sequential bytes are served; it must
// not discard the source's immutable size contract.
func (s *replaySession) Size(ctx context.Context) (int64, error) {
	if s == nil {
		return 0, errors.New("prefix replay session is closed")
	}
	s.mu.Lock()
	underlying := s.underlying
	s.mu.Unlock()
	if underlying == nil {
		return 0, errors.New("prefix replay session is closed")
	}
	sizer, ok := underlying.(access.Sizer)
	if !ok || sizer == nil {
		return 0, access.ErrCapabilityView
	}
	return sizer.Size(ctx)
}

// Snapshot delegates to the session the prefix was read from. A wrapper that
// answered for itself would report no content identity at all, and Host would
// read that as the source having changed under a job that never touched it.
func (s *replaySession) Snapshot(ctx context.Context) (access.Snapshot, error) {
	if s == nil {
		return access.Snapshot{}, errors.New("prefix replay session is closed")
	}
	s.mu.Lock()
	underlying := s.underlying
	s.mu.Unlock()
	if underlying == nil {
		return access.Snapshot{}, errors.New("prefix replay session is closed")
	}
	reporter, ok := access.SnapshotOf(underlying)
	if !ok {
		return access.Snapshot{}, access.ErrNoSnapshot
	}
	return reporter.Snapshot(ctx)
}

func (s *replaySession) Close() error {
	if s == nil {
		return nil
	}
	s.close.Do(func() {
		s.mu.Lock()
		var failures []error
		for index := s.chunk; index < len(s.chunks); index++ {
			handle := s.chunks[index]
			if err := protectedCall("", "replay/chunk-release", func() error {
				handle.Release()
				return nil
			}); err != nil {
				failures = append(failures, err)
			}
			s.chunks[index] = buffer.Handle{}
		}
		s.reader = nil
		underlying := s.underlying
		s.underlying = nil
		s.mu.Unlock()
		if underlying != nil {
			if err := protectedCall("", "access/session", func() error { return underlying.Close() }); err != nil {
				failures = append(failures, err)
			}
		}
		s.closeErr = errors.Join(failures...)
	})
	return s.closeErr
}
