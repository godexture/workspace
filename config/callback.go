package config

import "github.com/godexture/godec/diagnostic"

// A schema is built from functions a third party declares: accessors, codec
// operations, default factories, presets, and schema validators. A bug in one
// of them must become a diagnostic at a known path, not a panic that unwinds
// through package initialization, Host construction, or planning. Every entry
// from this package into declared code goes through the helpers below.
//
// The recovered value can hold the very secret the callback was handling, so
// only the operation name reaches a diagnostic, an error, or a detail map.
const (
	operationRead      = "read"
	operationWrite     = "write"
	operationDecode    = "decode"
	operationEncode    = "encode"
	operationCanonical = "canonical"
	operationNormalize = "normalize"
	operationValidate  = "validate"
	operationSchema    = "schema-validator"
	operationDefault   = "default-factory"
	operationPreset    = "preset"
)

const invalidText = "<invalid>"

type callbackPanic struct{ operation string }

func (e callbackPanic) Error() string { return "config " + e.operation + " panicked" }

// guardError converts a panic into target. It is deferred by callbacks that
// already report failure to their caller as an error.
func guardError(operation string, target *error) {
	if recover() != nil {
		*target = callbackPanic{operation: operation}
	}
}

// guardValue substitutes fallback for a callback that has no failure channel.
func guardValue[T any](fallback T, run func() T) (result T) {
	defer func() {
		if recover() != nil {
			result = fallback
		}
	}()
	return run()
}

// guardItems reports a panic as one error diagnostic at path, keeping the
// callback's own diagnostics when it returns normally.
func guardItems(operation string, path diagnostic.Path, run func() []diagnostic.Item) (items []diagnostic.Item) {
	defer func() {
		if recover() != nil {
			items = []diagnostic.Item{panicItem(operation, path)}
		}
	}()
	return run()
}

// guardNormalize keeps the input value when normalization panics so the
// remaining fields still resolve and the failure is reported once.
func guardNormalize[T any](operation string, path diagnostic.Path, value T, run func() (T, []diagnostic.Item)) (result T, items []diagnostic.Item) {
	defer func() {
		if recover() != nil {
			result = value
			items = []diagnostic.Item{panicItem(operation, path)}
		}
	}()
	return run()
}

func panicItem(operation string, path diagnostic.Path) diagnostic.Item {
	return diagnostic.NewItem(codeCallbackPanic, diagnostic.ErrorSeverity, path, "declared config "+operation+" operation panicked", map[string]string{"operation": operation})
}
