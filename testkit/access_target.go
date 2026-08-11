package testkit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/godexture/godec/access"
)

// AccessTarget is one fresh external storage target used by a Provider case.
// Seed, Read, and Residue let testkit own transaction assertions without
// exposing Host or Job plumbing to the fixture author.
type AccessTarget struct {
	reference access.Reference
	seed      func(context.Context, []byte) error
	read      func(context.Context) ([]byte, error)
	residue   func(context.Context) ([]string, error)
	close     func() error
}

// NewAccessTarget builds a fixture target. residue returns names of staged or
// temporary objects that should be empty after abort, commit, and Close.
func NewAccessTarget(
	reference access.Reference,
	seed func(context.Context, []byte) error,
	read func(context.Context) ([]byte, error),
	residue func(context.Context) ([]string, error),
	close func() error,
) AccessTarget {
	return AccessTarget{reference: reference, seed: seed, read: read, residue: residue, close: close}
}

// AccessFixture creates a fresh target for every Plan, cancellation, and
// execution scenario and carries the bytes presented to the Provider.
type AccessFixture struct {
	bytes []byte
	open  func(context.Context) (AccessTarget, error)
	state *accessFixtureState
}

type accessFixtureState struct {
	once     sync.Once
	close    func() error
	closeErr error
}

// AccessFixtureOf constructs a Provider fixture without exposing scheduler,
// queue, ownership, or Plan details to its author. Repeated open calls must
// describe the same logical Reference so Plan purity compares identical input.
func AccessFixtureOf(bytes []byte, open func(context.Context) (AccessTarget, error)) AccessFixture {
	return AccessFixture{bytes: append([]byte(nil), bytes...), open: open}
}

// ReadOnlyReference constructs a source fixture whose immutable byte image is
// already identified by reference. It is useful for content-addressed and
// value-encoded Provider references that have no external staging residue.
func ReadOnlyReference(reference access.Reference, value []byte) AccessFixture {
	expected := append([]byte(nil), value...)
	return AccessFixtureOf(expected, func(context.Context) (AccessTarget, error) {
		return NewAccessTarget(
			reference,
			func(_ context.Context, seeded []byte) error {
				if !bytes.Equal(seeded, expected) {
					return fmt.Errorf("read-only Reference seed = %x, want %x", seeded, expected)
				}
				return nil
			},
			func(context.Context) ([]byte, error) { return append([]byte(nil), expected...), nil },
			func(context.Context) ([]string, error) { return nil, nil },
			func() error { return nil },
		), nil
	})
}

// LocalFile constructs a hermetic local-file Provider fixture. Each scenario
// receives its own temporary directory and testkit removes it after checking
// that no transaction artifact remains.
func LocalFile(bytes []byte) AccessFixture {
	var setup sync.Once
	var directory string
	var path string
	var reference access.Reference
	var setupErr error
	state := &accessFixtureState{}
	state.close = func() error {
		if directory == "" {
			return nil
		}
		return os.RemoveAll(directory)
	}
	fixture := AccessFixtureOf(bytes, func(context.Context) (AccessTarget, error) {
		setup.Do(func() {
			directory, setupErr = os.MkdirTemp("", "godec-testkit-file-")
			if setupErr != nil {
				return
			}
			path = filepath.Join(directory, "payload.bin")
			reference, setupErr = localFileReference(path)
		})
		if setupErr != nil {
			return AccessTarget{}, setupErr
		}
		return NewAccessTarget(
			reference,
			func(_ context.Context, value []byte) error {
				return os.WriteFile(path, value, 0o600)
			},
			func(context.Context) ([]byte, error) {
				return os.ReadFile(path)
			},
			func(context.Context) ([]string, error) {
				return filepath.Glob(filepath.Join(directory, ".payload.bin.godec-*"))
			},
			func() error { return nil },
		), nil
	})
	fixture.state = state
	return fixture
}

func (f AccessFixture) valid() bool { return f.open != nil }

func (f AccessFixture) cloneBytes() []byte { return append([]byte(nil), f.bytes...) }

func (f AccessFixture) close() error {
	if f.state == nil {
		return nil
	}
	f.state.once.Do(func() {
		if f.state.close != nil {
			f.state.closeErr = f.state.close()
		}
	})
	return f.state.closeErr
}

func (t AccessTarget) valid() bool {
	return t.reference.Valid() && t.seed != nil && t.read != nil && t.residue != nil && t.close != nil
}

func (t AccessTarget) closeTarget() error {
	if t.close == nil {
		return nil
	}
	return t.close()
}

func localFileReference(path string) (access.Reference, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return access.Reference{}, err
	}
	value := filepath.ToSlash(absolute)
	if runtime.GOOS == "windows" && !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return access.Parse((&url.URL{Scheme: "file", Path: value}).String())
}

func closeAccessTarget(target AccessTarget, checks ...error) error {
	return errors.Join(append(checks, target.closeTarget())...)
}
