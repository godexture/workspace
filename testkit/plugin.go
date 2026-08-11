package testkit

import (
	"fmt"
	"testing"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/plugin"
)

const structuralScope = "structural validation only; typed cases are required for semantic conformance"

// Plugin validates the structure derivable from one definition: identities,
// descriptors, config schemas, traits, port shapes, and execution bindings.
// Passing Plugin is not semantic conformance; callers must also run the typed
// cases appropriate for every executable component. Owned declaration targets
// outside definition are checked only when the complete composition is built.
func Plugin(t testing.TB, definition plugin.Definition) {
	t.Helper()
	for _, item := range structuralProblems(definition) {
		t.Errorf("testkit.Plugin %s: %s", structuralScope, formatDiagnostic(item))
	}
}

func structuralProblems(definition plugin.Definition) []diagnostic.Item {
	_, err := host.New(host.Plugins(plugin.NewSet(definition)))
	items := host.Diagnostics(err)
	result := make([]diagnostic.Item, 0, len(items))
	for _, item := range items {
		if deferredCompositionTarget(item, definition.Identity()) {
			continue
		}
		result = append(result, item)
	}
	return result
}

func deferredCompositionTarget(item diagnostic.Item, owner plugin.Identity) bool {
	return !owner.IsZero() &&
		item.Code == "catalog.declaration-target" &&
		item.Detail["ownerDefinition"] == owner.String()
}

func formatDiagnostic(item diagnostic.Item) string {
	if path := item.Path.String(); path != "" {
		return fmt.Sprintf("%s: %s: %s", path, item.Code, item.Message)
	}
	return fmt.Sprintf("%s: %s", item.Code, item.Message)
}
