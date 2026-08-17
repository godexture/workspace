package errorx

import (
	"errors"
	"fmt"
	"testing"
)

type panicUnwrap struct{ cause error }

func (panicUnwrap) Error() string { return "malformed" }
func (panicUnwrap) Unwrap() error { panic("plugin unwrap") }

type cycleError struct{}

func (cycleError) Error() string      { return "cycle" }
func (c cycleError) Unwrap() error    { return c }
func (cycleError) StackTrace() []byte { return []byte("cycle-stack") }

type stackOnly struct{}

func (stackOnly) Error() string      { return "stack only" }
func (stackOnly) StackTrace() []byte { return []byte("not a recovered panic") }

type uncomparableError struct {
	child error
	data  []byte
}

func (u uncomparableError) Error() string { return "uncomparable" }
func (u uncomparableError) Unwrap() error { return u.child }

func TestInspectionContainsPanicAndCycles(t *testing.T) {
	wrapped := panicUnwrap{cause: errors.New("hidden")}
	if Is(wrapped, wrapped.cause) {
		t.Fatal("a panicking Unwrap exposed a child")
	}
	if _, ok := Find[error](wrapped); !ok {
		t.Fatal("the opaque error itself was not found")
	}
	if Stack(wrapped) != nil {
		t.Fatal("a panicking Unwrap produced a stack")
	}

	cycle := cycleError{}
	if Is(cycle, errors.New("missing")) {
		t.Fatal("an unrelated target matched through a cycle")
	}
	if stack := Stack(cycle); string(stack) != "cycle-stack" {
		t.Fatalf("cycle stack = %q, want the current value without looping", stack)
	}
}

func TestRecoveredPanicRequiresPrivateMarker(t *testing.T) {
	if _, ok := RecoveredPanic(stackOnly{}); ok {
		t.Fatal("an arbitrary StackTrace error was classified as a recovered panic")
	}
	want := errors.New("closed with panic")
	marked := MarkPanic(want, []byte("panic stack"))
	if !errors.Is(marked, want) {
		t.Fatal("panic marker lost its original error")
	}
	stack, ok := RecoveredPanic(errors.Join(errors.New("peer"), marked))
	if !ok || string(stack) != "panic stack" {
		t.Fatalf("recovered marker = %q, %v", stack, ok)
	}
}

func TestOnlyRequiresAPureSingleUnwrapChain(t *testing.T) {
	target := errors.New("target")
	if !Only(fmt.Errorf("context: %w", target), target) {
		t.Fatal("a single wrapper did not preserve the target identity")
	}
	if Only(errors.Join(target, errors.New("independent")), target) {
		t.Fatal("a joined error was treated as a pure propagation")
	}
	if Only(fmt.Errorf("context: %w", errors.Join(target, errors.New("independent"))), target) {
		t.Fatal("a wrapper around a joined error was treated as pure")
	}
	if Only(panicUnwrap{cause: target}, target) {
		t.Fatal("a panicking unwrap was treated as pure")
	}
	cycle := cycleError{}
	if Only(cycle, cycle) {
		t.Fatal("a cyclic unwrap was treated as pure")
	}
	if !Only(uncomparableError{child: target, data: []byte{1}}, target) {
		t.Fatal("an uncomparable wrapper did not safely reach its target")
	}
}
