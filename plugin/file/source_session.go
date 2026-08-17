package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/godexture/godec/access"
)

var errSessionClosed = errors.New("file Access session is closed")

// sourceSession serves the file it opened, not the path it opened it from. The
// handle keeps the original file object alive across a rename or a replacement
// of the path, so those are not content changes for this session.
//
// StableSize is a promise about the bytes this session hands out, so reads stop
// at the size recorded when the file was acquired even if another writer grows
// it. Whether a change between phases ends the job is Host's decision, made
// from the snapshot identity below.
type sourceSession struct {
	mu       sync.Mutex
	handle   *os.File
	size     int64
	modified int64
	position int64
	closed   bool
}

func sourceCapabilities() access.Capabilities {
	capabilities, err := access.NewCapabilities(
		access.SequentialRead,
		access.RandomRead,
		access.StableSize,
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
		return nil, redactIO("open", err)
	}
	info, err := handle.Stat()
	if err != nil {
		return nil, errors.Join(redactIO("stat", err), redactIO("close", handle.Close()))
	}
	return &sourceSession{handle: handle, size: info.Size(), modified: info.ModTime().UnixNano()}, nil
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
	destination, err := s.bounded(destination, s.position)
	if err != nil {
		return 0, err
	}
	count, err := s.handle.Read(destination)
	s.position += int64(count)
	if cause := contextFailure(ctx); cause != nil {
		return count, cause
	}
	return count, redactIO("read", err)
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
	destination, err := s.bounded(destination, offset)
	if err != nil {
		return 0, err
	}
	count, err := s.handle.ReadAt(destination, offset)
	if cause := contextFailure(ctx); cause != nil {
		return count, cause
	}
	return count, redactIO("read-at", err)
}

func (s *sourceSession) Size(ctx context.Context) (int64, error) {
	if err := contextFailure(ctx); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.handle == nil {
		return 0, errSessionClosed
	}
	return s.size, nil
}

// Snapshot reports what the file looks like now. Size and modification time
// are a weak identity: they catch a truncate, a grow, and an overwrite that
// moves the timestamp, but two same-size writes inside one timestamp tick are
// indistinguishable. A local file has no cheaper strong identity than reading
// it, so the nature is reported honestly rather than overstated.
func (s *sourceSession) Snapshot(ctx context.Context) (access.Snapshot, error) {
	if err := contextFailure(ctx); err != nil {
		return access.Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.handle == nil {
		return access.Snapshot{}, errSessionClosed
	}
	info, err := s.handle.Stat()
	if err != nil {
		return access.Snapshot{}, redactIO("stat", err)
	}
	return access.NewSnapshot(snapshotIdentity(info.Size(), info.ModTime().UnixNano()), access.WeakSnapshot)
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
	err := redactIO("close", s.handle.Close())
	s.handle = nil
	return err
}

// bounded limits one read to the size this session promised. A read that
// starts past that size reports EOF rather than returning bytes a later writer
// appended, which would contradict the size Inspect planned against.
func (s *sourceSession) bounded(destination []byte, offset int64) ([]byte, error) {
	if offset < 0 {
		return nil, fmt.Errorf("file Access read offset %d is negative", offset)
	}
	if offset >= s.size {
		return nil, io.EOF
	}
	if remaining := s.size - offset; int64(len(destination)) > remaining {
		return destination[:remaining], nil
	}
	return destination, nil
}

func snapshotIdentity(size, modified int64) string {
	return fmt.Sprintf("file/size:%d/mtime:%d", size, modified)
}

func contextFailure(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return context.Cause(ctx)
}
