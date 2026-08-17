package file

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/godexture/godec/access"
)

type sinkState uint8

const (
	sinkWriting sinkState = iota + 1
	sinkPrepared
	sinkCommitted
	sinkAborted
)

type sinkSession struct {
	mu     sync.Mutex
	handle *os.File
	target string
	temp   string
	state  sinkState
}

func sinkCapabilities() access.Capabilities {
	capabilities, err := access.NewCapabilities(access.SequentialWrite, access.RandomWrite)
	if err != nil {
		panic(err)
	}
	return capabilities
}

func acquireSink(ctx context.Context, reference access.Reference, selected access.Selection) (access.Session, error) {
	if err := contextFailure(ctx); err != nil {
		return nil, err
	}
	if !selected.ValidFor(access.SinkDirection) {
		return nil, access.ErrInvalidCapabilities
	}
	target, err := pathOf(reference)
	if err != nil {
		return nil, err
	}
	handle, err := createTemporary(target)
	if err != nil {
		return nil, err
	}
	return &sinkSession{handle: handle, target: target, temp: handle.Name(), state: sinkWriting}, nil
}

func (*sinkSession) Capabilities() access.Capabilities { return sinkCapabilities() }

func (s *sinkSession) Write(ctx context.Context, source []byte) (int, error) {
	if err := contextFailure(ctx); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != sinkWriting || s.handle == nil {
		return 0, errSessionClosed
	}
	count, err := s.handle.Write(source)
	if cause := contextFailure(ctx); cause != nil {
		return count, cause
	}
	return count, redactIO("write", err)
}

func (s *sinkSession) WriteAt(ctx context.Context, source []byte, offset int64) (int, error) {
	if err := contextFailure(ctx); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != sinkWriting || s.handle == nil {
		return 0, errSessionClosed
	}
	count, err := s.handle.WriteAt(source, offset)
	if cause := contextFailure(ctx); cause != nil {
		return count, cause
	}
	return count, redactIO("write-at", err)
}

func (s *sinkSession) Flush(ctx context.Context) error {
	if err := contextFailure(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != sinkWriting || s.handle == nil {
		return errSessionClosed
	}
	return nil
}

func (s *sinkSession) Sync(ctx context.Context) error {
	if err := contextFailure(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != sinkWriting || s.handle == nil {
		return errSessionClosed
	}
	return redactIO("sync", s.handle.Sync())
}

func (s *sinkSession) PrepareCommit(ctx context.Context) error {
	if err := contextFailure(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == sinkPrepared {
		return nil
	}
	if s.state != sinkWriting || s.handle == nil {
		return errSessionClosed
	}
	err := redactIO("close", s.handle.Close())
	s.handle = nil
	if err != nil {
		return err
	}
	s.state = sinkPrepared
	return nil
}

func (s *sinkSession) Commit(ctx context.Context) error {
	if err := contextFailure(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != sinkPrepared || s.temp == "" {
		return errSessionClosed
	}
	if err := preservePermissions(s.temp, s.target); err != nil {
		return err
	}
	if err := redactIO("rename", os.Rename(s.temp, s.target)); err != nil {
		return err
	}
	s.temp = ""
	s.state = sinkCommitted
	return syncDirectory(s.target)
}

func (s *sinkSession) Abort(ctx context.Context) error {
	if err := contextFailure(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == sinkCommitted || s.state == sinkAborted {
		return nil
	}
	var failures []error
	if s.handle != nil {
		failures = append(failures, redactIO("close", s.handle.Close()))
		s.handle = nil
	}
	if s.temp != "" {
		if err := redactIO("remove", os.Remove(s.temp)); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, err)
		}
		s.temp = ""
	}
	s.state = sinkAborted
	return errors.Join(failures...)
}

func (s *sinkSession) Close() error {
	return s.Abort(context.Background())
}
