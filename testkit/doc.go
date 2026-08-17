// Package testkit provides public structural and typed conformance helpers for
// plugin authors. Plugin performs structural validation only; semantic
// conformance requires the typed case helpers in this package.
//
// A successful Case also exercises cancellation after data has reached the
// subject callback (or Access session), then verifies Run termination and
// Close. Cases that expect planning or run failure cancel before execution
// because callback reachability is not part of that expectation.
package testkit
