package file

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/godexture/godec/access"
)

var errSessionClosed = errors.New("file Access session is closed")

type sourceSession struct {
	mu     sync.Mutex
	handle *os.File
	closed bool
}

func sourceCapabilities() access.Capabilities {
	capabilities, err := access.NewCapabilities(
		access.SequentialRead,
		access.RandomRead,
		access.StableSize,
		access.Reopen,
		access.CancelableRead,
	)
	if err != nil {
		panic(err)
	}
	return capabilities
}

func acquireSource(ctx context.Context, reference access.Reference, selected access.Selection) (access.Session, error) {
	if err := contextFailure(ctx); err != nil {
		return nil, err
	}
	if !selected.ValidFor(access.SourceDirection) {
		return nil, access.ErrInvalidCapabilities
	}
	path, err := pathOf(reference)
	if err != nil {
		return nil, err
	}
	handle, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &sourceSession{handle: handle}, nil
}

func (*sourceSession) Capabilities() access.Capabilities { return sourceCapabilities() }

func (s *sourceSession) Read(ctx context.Context, destination []byte) (int, error) {
	if err := contextFailure(ctx); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.handle == nil {
		return 0, errSessionClosed
	}
	count, err := s.handle.Read(destination)
	if cause := contextFailure(ctx); cause != nil {
		return count, cause
	}
	return count, err
}

func (s *sourceSession) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if err := contextFailure(ctx); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.handle == nil {
		return 0, errSessionClosed
	}
	count, err := s.handle.ReadAt(destination, offset)
	if cause := contextFailure(ctx); cause != nil {
		return count, cause
	}
	return count, err
}

func (s *sourceSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.handle == nil {
		return nil
	}
	err := s.handle.Close()
	s.handle = nil
	return err
}

func contextFailure(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return context.Cause(ctx)
}
