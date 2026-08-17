package journal

import "github.com/godexture/godec/internal/errorx"

// A plugin owns its error implementation, including Unwrap. Ledger inspection
// never runs that code while holding a ledger or domain lock. A malformed
// implementation is treated as an opaque occurrence: it cannot strand a span
// ticket or make the ledger's own state unusable.
const unwrapLimit = errorx.Limit

// branches splits an error graph at every reachable multi-error. Single
// wrappers are context for one occurrence, except when they merely surround a
// join: then the join's branches remain independent occurrences.
func branches(err error) []error {
	parts, split := branch(err, 0)
	if !split || len(parts) == 0 {
		return []error{err}
	}
	return parts
}

func branch(err error, depth int) ([]error, bool) {
	if err == nil || depth >= unwrapLimit {
		return nil, false
	}
	if parts, ok := errorx.UnwrapMany(err); ok {
		if len(parts) == 0 {
			return nil, false
		}
		result := make([]error, 0, len(parts))
		for _, part := range parts {
			children, split := branch(part, depth+1)
			if split {
				result = append(result, children...)
			} else if part != nil {
				result = append(result, part)
			}
		}
		if len(result) == 0 {
			return nil, false
		}
		return result, true
	}
	if child, ok := errorx.UnwrapOne(err); ok && child != nil {
		if result, split := branch(child, depth+1); split {
			return result, true
		}
	}
	return nil, false
}

// causeOf reports the event a branch re-propagates, if the branch is nothing
// but that re-propagation. A multi-error is never one occurrence, and is
// rejected here even when callers bypass branches.
func causeOf(err error) *Cause {
	for depth := 0; err != nil && depth < unwrapLimit; depth++ {
		if cause, ok := err.(*Cause); ok {
			return cause
		}
		if _, joined := errorx.UnwrapMany(err); joined {
			return nil
		}
		wrapped, ok := errorx.UnwrapOne(err)
		if !ok {
			return nil
		}
		err = wrapped
	}
	return nil
}
