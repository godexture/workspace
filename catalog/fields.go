package catalog

import (
	"slices"

	"github.com/godexture/core/registry"
	"github.com/godexture/sdk/cliflag"
)

type Field struct {
	Name      string           `json:"name"`
	Type      string           `json:"type"`
	Help      string           `json:"help"`
	Default   string           `json:"default"`
	Choices   []string         `json:"choices,omitempty"`
	DependsOn *FieldDependency `json:"dependsOn,omitempty"`
}

type FieldDependency struct {
	Field  string   `json:"field"`
	Values []string `json:"values"`
}

// fields describes a manifest's configuration fields for the catalog. It
// reads value.Default() rather than NewConfiguration() because a default
// config isn't necessarily valid on its own (e.g. remix has no sensible
// default target channel layout) -- describing its shape shouldn't depend
// on it passing the same Validate() a real conversion would require.
func fields(value registry.Manifest) ([]Field, error) {
	described, err := cliflag.DescribeStruct(value.Default())
	if err != nil {
		return nil, err
	}
	result := make([]Field, len(described))
	for i, field := range described {
		var dependsOn *FieldDependency
		if field.DependsOn != nil {
			dependsOn = &FieldDependency{Field: field.DependsOn.Field, Values: slices.Clone(field.DependsOn.Values)}
		}
		result[i] = Field{Name: field.Name, Type: field.Type, Help: field.Help, Default: field.Default, Choices: slices.Clone(field.Choices), DependsOn: dependsOn}
	}
	return result, nil
}
