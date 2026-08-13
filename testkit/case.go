package testkit

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
)

// Case contains only the component config, typed input fixture, and expected
// result. Host, Job, Plan, queues, grants, and ownership remain testkit-owned.
type Case[I, O any] struct {
	Name   string
	Config config.Patch
	Input  Fixture[I]
	Want   Expectation[O]
}

// Fixture owns a typed sequence and its stream descriptor. Values takes an
// independent schema-level share of every supplied value; the caller retains
// ownership of the values it passed.
type Fixture[T any] struct {
	descriptor stream.Descriptor
	typ        schema.Type[T]
	values     []T
	owners     *owners
	verify     func() error
}

// Values constructs an input fixture from typed media items.
func Values[T any](descriptor stream.Descriptor, typ schema.Type[T], values ...T) Fixture[T] {
	owned := make([]T, len(values))
	for index, value := range values {
		owned[index] = typ.Fork(value)
	}
	return Fixture[T]{descriptor: descriptor, typ: typ, values: owned, owners: &owners{}}
}

func (f Fixture[T]) valid() bool {
	return f.descriptor.Valid() && f.typ.Valid() && f.descriptor.Schema() == f.typ.Identity()
}

func (f Fixture[T]) clone() Fixture[T] {
	result := Fixture[T]{descriptor: f.descriptor, typ: f.typ, values: make([]T, len(f.values)), owners: &owners{}}
	for index, value := range f.values {
		result.values[index] = f.typ.Fork(value)
	}
	return result
}

// emit hands the next unread value to the runtime under ownership accounting.
func (f *Fixture[T]) emit(index int) flow.Item[T] {
	value := f.values[index]
	var zero T
	f.values[index] = zero
	return ownedItem(value, f.typ, f.owners)
}

func (f *Fixture[T]) close() error {
	if f == nil {
		return nil
	}
	for index, value := range f.values {
		f.typ.Drop(value)
		var zero T
		f.values[index] = zero
	}
	f.values = nil
	problems := []error{f.owners.verify()}
	if f.verify != nil {
		problems = append(problems, f.verify())
	}
	return errors.Join(problems...)
}

type recorder[T any] interface {
	accept(T)
	finish() error
}

// Expectation describes either logical output values or a diagnostic code.
// Values are projected synchronously while the runtime item is borrowed, so
// the sink can release every payload before Host verifies its grants.
type Expectation[T any] struct {
	newRecorder func() recorder[T]
	failure     failureExpectation
}

type failureStage uint8

const (
	planFailure failureStage = iota + 1
	runFailure
)

type failureExpectation struct {
	stage  failureStage
	code   string
	target error
}

// WantValues compares projected logical values in order. snapshot must copy
// every part that would become invalid when the runtime item is released.
func WantValues[T, V any](want []V, snapshot func(T) (V, error)) Expectation[T] {
	wantCopy := append([]V(nil), want...)
	return Expectation[T]{newRecorder: func() recorder[T] {
		return &valueRecorder[T, V]{want: append([]V(nil), wantCopy...), snapshot: snapshot}
	}}
}

// EqualValues compares directly comparable runtime values in order.
func EqualValues[T comparable](want ...T) Expectation[T] {
	return WantValues(append([]T(nil), want...), func(value T) (T, error) { return value, nil })
}

// Fails expects execution to report diagnostic code. Cleanup and ownership
// accounting must still succeed.
func Fails[T any](code string) Expectation[T] {
	return Expectation[T]{
		failure:     failureExpectation{stage: runFailure, code: code},
		newRecorder: func() recorder[T] { return &discardRecorder[T]{} },
	}
}

// WantPlanError expects Host.Plan to reject the fixture with target. It is
// intended for control-plane validation such as malformed Format headers.
func WantPlanError[T any](target error) Expectation[T] {
	return Expectation[T]{
		failure:     failureExpectation{stage: planFailure, target: target},
		newRecorder: func() recorder[T] { return &discardRecorder[T]{} },
	}
}

// WantRunError expects execution to fail with target after planning and
// preparation succeeded.
func WantRunError[T any](target error) Expectation[T] {
	return Expectation[T]{
		failure:     failureExpectation{stage: runFailure, target: target},
		newRecorder: func() recorder[T] { return &discardRecorder[T]{} },
	}
}

func (e Expectation[T]) valid() bool {
	if e.newRecorder == nil {
		return false
	}
	failure := e.failure
	if failure.stage == 0 {
		return failure.code == "" && failure.target == nil
	}
	if failure.stage != planFailure && failure.stage != runFailure {
		return false
	}
	if failure.code != "" {
		return failure.target == nil && failure.code == strings.TrimSpace(failure.code)
	}
	return failure.target != nil
}

type valueRecorder[T, V any] struct {
	mu       sync.Mutex
	want     []V
	got      []V
	snapshot func(T) (V, error)
	problems []error
}

func (r *valueRecorder[T, V]) accept(value T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.snapshot == nil {
		r.problems = append(r.problems, errors.New("output snapshot function is nil"))
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			r.problems = append(r.problems, fmt.Errorf("output snapshot panicked: %s", diagnostic.Recovered(recovered)))
		}
	}()
	projected, err := r.snapshot(value)
	if err != nil {
		r.problems = append(r.problems, err)
		return
	}
	r.got = append(r.got, projected)
}

func (r *valueRecorder[T, V]) finish() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.problems) != 0 {
		return errors.Join(r.problems...)
	}
	if !equalLogicalSlices(r.got, r.want) {
		return fmt.Errorf("logical output mismatch: got %#v, want %#v", r.got, r.want)
	}
	return nil
}

func equalLogicalSlices[T any](left, right []T) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !reflect.DeepEqual(left[index], right[index]) {
			return false
		}
	}
	return true
}

type discardRecorder[T any] struct{}

func (*discardRecorder[T]) accept(T)      {}
func (*discardRecorder[T]) finish() error { return nil }
