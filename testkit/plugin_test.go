package testkit

import (
	"strings"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/plugin"
)

type structuralPluginID struct{}
type structuralComponentID struct{}
type structuralConfigID struct{}
type structuralTraitID struct{}
type structuralDeclarationID struct{}
type externalComponentID struct{}

type structuralConfig struct{}

func TestStructuralProblemsAcceptValidDefinition(t *testing.T) {
	definition := structuralDefinition(plugin.PortShapeOptional)
	if items := structuralProblems(definition); len(items) != 0 {
		t.Fatalf("valid definition diagnostics = %v", items)
	}
	Plugin(t, definition)
}

func TestStructuralProblemsRejectRequiredTraitWithoutPorts(t *testing.T) {
	items := structuralProblems(structuralDefinition(plugin.PortShapeRequired))
	if !hasStructuralCode(items, "catalog.trait-shape") {
		t.Fatalf("required trait diagnostics = %v", items)
	}
	message := "testkit.Plugin " + structuralScope + ": " + formatDiagnostic(items[0])
	if !strings.Contains(message, "structural validation only") || !strings.Contains(message, "typed cases are required") {
		t.Fatalf("failure scope is unclear: %q", message)
	}
}

func TestStructuralProblemsDeferOwnedExternalTarget(t *testing.T) {
	definition := structuralDefinition(plugin.PortShapeOptional).WithDeclarations(
		plugin.Declare[structuralDeclarationID]("external", plugin.IdentityOf[externalComponentID]()),
	)
	if items := structuralProblems(definition); len(items) != 0 {
		t.Fatalf("external composition target was treated as a definition error: %v", items)
	}
}

func TestStructuralProblemsRejectEmptyDefinition(t *testing.T) {
	definition := plugin.Define[structuralPluginID](plugin.Descriptor{DisplayName: "empty", Version: "1"})
	if items := structuralProblems(definition); !hasStructuralCode(items, "plugin.empty-definition") {
		t.Fatalf("empty definition diagnostics = %v", items)
	}
}

func structuralDefinition(requirement plugin.TraitShapeRequirement) plugin.Definition {
	schema := config.Struct[structuralConfigID](func() structuralConfig { return structuralConfig{} }).Version("1").Build()
	component := plugin.NewComponent[structuralComponentID](
		plugin.Descriptor{DisplayName: "structural component"},
		schema,
		plugin.WithTrait(plugin.TraitKeyOf[structuralTraitID](), "fixture=true", requirement, struct{}{}),
	)
	return plugin.Define[structuralPluginID](plugin.Descriptor{DisplayName: "structural plugin", Version: "1"}, component)
}

func hasStructuralCode(items []diagnostic.Item, code string) bool {
	for _, item := range items {
		if item.Code == code {
			return true
		}
	}
	return false
}
