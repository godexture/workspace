package host

import "context"

// invokeAccess is the single boundary for calls into a provider-owned Access
// operation.  Access callbacks are not trusted merely because they return an
// error: a panic must become the same phase/node/task failure as every other
// Host callback, while preserving the operation's result only on success.
func invokeAccess(ctx context.Context, phase Phase, node, task string, work func(context.Context) error) *Failure {
	return invoke(ctx, phase, node, task, work)
}

// callAccess adapts the common value-returning Access callbacks (Snapshot,
// Read, and ReadAt) to invokeAccess without duplicating a recovery boundary at
// each call site.
func callAccess[T any](ctx context.Context, phase Phase, node, task string, work func(context.Context) (T, error)) (value T, failure *Failure) {
	failure = invokeAccess(ctx, phase, node, task, func(callContext context.Context) error {
		var err error
		value, err = work(callContext)
		return err
	})
	return value, failure
}
