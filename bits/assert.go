//go:build !production

package bits

import "fmt"

// assertf checks a programmer invariant (not a data-driven condition) and
// panics with a formatted message if it does not hold. It compiles away to
// a no-op in production builds (see assert_production.go).
func assertf(cond bool, format string, args ...any) {
	if !cond {
		panic(fmt.Sprintf(format, args...))
	}
}
