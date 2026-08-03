package diagnostic_test

import (
	"fmt"

	"github.com/godexture/godec/diagnostic"
)

// An aggregate reports every problem it collected instead of stopping at the
// first one, so one construction attempt surfaces all of a caller's mistakes.
func ExampleError() {
	err := diagnostic.NewError(
		diagnostic.NewItem(
			"config.range",
			diagnostic.ErrorSeverity,
			diagnostic.FieldPath("compression"),
			"value is outside the allowed range",
			map[string]string{"expected": "0..8"},
		),
		diagnostic.NewItem(
			"config.unknown-field",
			diagnostic.ErrorSeverity,
			diagnostic.FieldPath("verfy"),
			"field is not registered by this schema",
			nil,
		).WithSuggestions([]string{"verify"}),
	)

	fmt.Println(err)
	// Output:
	// compression: error: config.range: value is outside the allowed range
	// verfy: error: config.unknown-field: field is not registered by this schema; did you mean "verify"?
}

// Callers that need the structured form read the items back out of any error
// in the chain.
func ExampleItemsOf() {
	err := diagnostic.NewError(diagnostic.NewItem(
		"plugin.descriptor.version",
		diagnostic.ErrorSeverity,
		diagnostic.DescriptorPath("acme.codec", "version"),
		"descriptor version is required",
		nil,
	))

	for _, item := range diagnostic.ItemsOf(err) {
		fmt.Println(item.Code, item.Path)
	}
	// Output: plugin.descriptor.version acme.codec.descriptor.version
}
