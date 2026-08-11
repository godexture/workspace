package testkit

import "testing"

// Component runs the common typed component contract.
func Component[I, O any](t testing.TB, subject Subject[I, O], cases ...Case[I, O]) {
	t.Helper()
	runCases(t, componentRunner, subject, cases)
}

// Codec runs the common contract for a codec or parser component. Codec
// helpers add media-specific cases without duplicating the lifecycle runner.
func Codec[I, O any](t testing.TB, subject Subject[I, O], cases ...Case[I, O]) {
	t.Helper()
	runCases(t, codecRunner, subject, cases)
}

// Format runs the common contract with the subject attached directly to a
// byte Access boundary, so Prepare performs Inspect before Compile.
func Format[I, O any](t testing.TB, subject Subject[I, O], cases ...Case[I, O]) {
	t.Helper()
	runCases(t, formatRunner, subject, cases)
}
