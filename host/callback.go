package host

import "runtime/debug"

// protectedCall is the small recovery boundary used when one host-owned
// composite operation invokes a provider-owned child. The caller can continue
// with the remaining children after one callback panics; the returned panic
// error is deliberately value-free and can be joined with ordinary errors.
func protectedCall(node, task string, work func() error) (err error) {
	completed := false
	defer func() {
		recovered := recover()
		if !completed {
			err = panicError(task, node, recovered, debug.Stack())
		}
	}()
	err = work()
	completed = true
	return err
}
