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

func fields(value registry.Manifest) ([]Field, error) {
	config, err := value.NewConfiguration()
	if err != nil {
		return nil, err
	}
	described, err := cliflag.DescribeStruct(config)
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
