// Link is the typed edge between two operators, and Scope names the node an
// edge belongs to for diagnostics.
package drive

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/godexture/godec/flow"
)

type closer interface {
	close(context.Context) error
}

type scopeBinder interface{ bindScope(*Scope) }

// Scope carries what a task cannot tell its boundary by returning: where it
// was, and what it could not release on the way out. One task and the delivery
// chain it drives share one Scope. It intentionally uses no atomics because
// that task is the only writer and panic recovery reads it in the same
// goroutine.
//
// It carries no item cleanup: every cell is released by the deferred Drop of
// the task that owns it, which also runs while a panic unwinds. What it carries
// is the failures of those releases.
type Scope struct {
	node     string
	failures []error
}

func NewScope(node string) *Scope { return &Scope{node: node} }
func (s *Scope) Node() string {
	if s == nil {
		return ""
	}
	return s.node
}

// fail records a failure found while releasing and hands it back unchanged, so
// the call site reports it the ordinary way too. Recording is unconditional
// and reading is not: a task that returns carries the failure out itself, and
// only a panic that discarded that return makes the boundary read this.
func (s *Scope) fail(err error) error {
	if s != nil && err != nil {
		s.failures = append(s.failures, err)
	}
	return err
}

// Cleanup reports what releasing could not do, for the boundary that recovered
// the panic which lost the task's return value.
func (s *Scope) Cleanup() error {
	if s == nil {
		return nil
	}
	return errors.Join(s.failures...)
}

func (s *Scope) set(node string) {
	if s != nil {
		s.node = node
	}
}

type delivery[T any] interface {
	flow.Emitter[T]
	closer
}

// Link is an opaque typed delivery endpoint used only while Program is
// specialized. Its payload never becomes any during delivery.
type Link struct {
	payload reflect.Type
	value   any
}

func (l Link) Valid() bool { return l.payload != nil && l.value != nil }

func (l Link) Close(ctx context.Context) error {
	if !l.Valid() {
		return ErrLink
	}
	return l.value.(closer).close(ctx)
}

func (l Link) BindScope(scope *Scope) {
	if !l.Valid() {
		return
	}
	if value, ok := l.value.(scopeBinder); ok {
		value.bindScope(scope)
	}
}

func linkOf[T any](value delivery[T]) Link {
	return Link{payload: reflect.TypeFor[T](), value: value}
}

func deliveryOf[T any](link Link) (delivery[T], error) {
	if link.payload != reflect.TypeFor[T]() {
		return nil, fmt.Errorf("%w: want %s, got %v", ErrLink, reflect.TypeFor[T](), link.payload)
	}
	value, ok := link.value.(delivery[T])
	if !ok {
		return nil, fmt.Errorf("%w: link contains %T", ErrLink, link.value)
	}
	return value, nil
}

// Task is a top-level execution loop. Host runs it through the tracked task
// group, which owns panic recovery, cancellation, and join.
