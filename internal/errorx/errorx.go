// Package errorx provides bounded, panic-safe inspection of third-party error
// graphs.  Error values cross plugin boundaries, so even Unwrap and As are
// callbacks owned by code outside the runtime.  The standard errors helpers
// deliberately do not put a bound around those callbacks; these helpers do.
package errorx

import "reflect"

type recoveredPanic struct {
	err   error
	stack []byte
}

func (e *recoveredPanic) Error() string { return e.err.Error() }
func (e *recoveredPanic) Unwrap() error { return e.err }
func (e *recoveredPanic) StackTrace() []byte {
	if e == nil {
		return nil
	}
	return append([]byte(nil), e.stack...)
}

// MarkPanic preserves a recovered panic without exposing a concrete marker
// that third-party errors can implement accidentally.
func MarkPanic(err error, stack []byte) error {
	if err == nil {
		return nil
	}
	return &recoveredPanic{err: err, stack: append([]byte(nil), stack...)}
}

// RecoveredPanic reports only markers created by MarkPanic. StackTrace alone
// is evidence for rendering, not proof that an error came from a panic.
func RecoveredPanic(err error) (stack []byte, ok bool) {
	walk(err, func(current error) bool {
		marked, match := current.(*recoveredPanic)
		if !match || marked == nil {
			return false
		}
		stack = marked.StackTrace()
		ok = true
		return true
	})
	return stack, ok
}

// Limit bounds one inspection walk.  It is intentionally finite: an error
// graph is third-party input, and a cycle must not hold a lifecycle boundary
// open forever.
const Limit = 128

// Is is the bounded, panic-safe counterpart of errors.Is.  It preserves the
// direct equality and Is(error) hooks used by ordinary errors, while treating
// a panicking or over-deep graph as an opaque branch.
func Is(err, target error) bool {
	if err == nil || target == nil {
		return err == target
	}
	return walk(err, func(current error) bool {
		if equal(current, target) {
			return true
		}
		matcher, ok := current.(interface{ Is(error) bool })
		return ok && callIs(matcher, target)
	})
}

// Only reports whether target occurs in a finite, single-error unwrap chain.
// It deliberately rejects joined errors, malformed unwraps, cycles, and
// chains deeper than Limit. This is useful for distinguishing a pure
// propagation of one sentinel from an error graph that also carries an
// independent occurrence; errors.Is cannot make that distinction.
func Only(err, target error) bool {
	if err == nil || target == nil {
		return false
	}
	seen := make([]error, 0, 4)
	matched := false
	for depth := 0; depth < Limit && err != nil; depth++ {
		for _, prior := range seen {
			if equal(prior, err) {
				return false
			}
		}
		seen = append(seen, err)
		if equal(err, target) {
			matched = true
		}
		if _, joined := err.(interface{ Unwrap() []error }); joined {
			return false
		}
		if _, wrapped := err.(interface{ Unwrap() error }); wrapped {
			child, ok := UnwrapOne(err)
			if !ok {
				return false
			}
			err = child
			continue
		}
		return matched
	}
	return false
}

// Find returns the first value assignable to T in an error graph.  A custom
// As(any) hook is honoured when it completes normally; a panic is treated as
// an opaque value.  Traversal is bounded so an accidental cycle cannot strand
// a lifecycle boundary.
func Find[T any](err error) (value T, ok bool) {
	return find(err, func(current error) (T, bool) {
		if typed, matched := current.(T); matched {
			return typed, true
		}
		var candidate T
		as, implements := current.(interface{ As(any) bool })
		if implements && callAs(as, &candidate) {
			return candidate, true
		}
		var zero T
		return zero, false
	})
}

// Stack returns a copied stack from the first StackTrace-bearing value in an
// error graph.  StackTrace is third-party code too, so a panic is treated as no
// stack rather than escaping the boundary.
func Stack(err error) (stack []byte) {
	value, ok := Find[interface{ StackTrace() []byte }](err)
	if !ok || value == nil {
		return nil
	}
	defer func() {
		if recover() != nil {
			stack = nil
		}
	}()
	return append([]byte(nil), value.StackTrace()...)
}

func find[T any](err error, match func(error) (T, bool)) (value T, ok bool) {
	walk(err, func(current error) bool {
		if found, matched := match(current); matched {
			value, ok = found, true
			return true
		}
		return false
	})
	return value, ok
}

func walk(root error, visit func(error) bool) bool {
	if root == nil {
		return false
	}
	type node struct {
		err   error
		depth int
	}
	stack := []node{{err: root}}
	for len(stack) != 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		if current.err == nil {
			continue
		}
		if visit(current.err) {
			return true
		}
		if current.depth >= Limit {
			continue
		}
		if children, ok := UnwrapMany(current.err); ok {
			for index := len(children) - 1; index >= 0; index-- {
				if children[index] != nil {
					stack = append(stack, node{err: children[index], depth: current.depth + 1})
				}
			}
			continue
		}
		if child, ok := UnwrapOne(current.err); ok && child != nil {
			stack = append(stack, node{err: child, depth: current.depth + 1})
		}
	}
	return false
}

func equal(left, right error) bool {
	leftType, rightType := reflect.TypeOf(left), reflect.TypeOf(right)
	if leftType == nil || rightType == nil || leftType != rightType || !leftType.Comparable() {
		return false
	}
	return left == right
}

func callIs(matcher interface{ Is(error) bool }, target error) (matched bool) {
	defer func() {
		if recover() != nil {
			matched = false
		}
	}()
	return matcher.Is(target)
}

func callAs(as interface{ As(any) bool }, target any) (matched bool) {
	defer func() {
		if recover() != nil {
			matched = false
		}
	}()
	return as.As(target)
}

// UnwrapOne invokes a single-error Unwrap method with panic recovery.
func UnwrapOne(err error) (child error, ok bool) {
	wrapped, implements := err.(interface{ Unwrap() error })
	if !implements {
		return nil, false
	}
	defer func() {
		if recover() != nil {
			child, ok = nil, false
		}
	}()
	return wrapped.Unwrap(), true
}

// UnwrapMany invokes a multi-error Unwrap method with panic recovery.
func UnwrapMany(err error) (children []error, ok bool) {
	joined, implements := err.(interface{ Unwrap() []error })
	if !implements {
		return nil, false
	}
	defer func() {
		if recover() != nil {
			children, ok = nil, false
		}
	}()
	return joined.Unwrap(), true
}
