//go:build production

package bits

// assertf is a no-op in production builds; see assert.go for the checked
// variant used by default.
func assertf(cond bool, format string, args ...any) {}
