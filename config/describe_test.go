package config

import (
	"reflect"
	"testing"
)

func TestDescriptionIsImmutable(t *testing.T) {
	builder := Struct[describeMarker](func() struct{ Mode int } { return struct{ Mode int }{} }).Version("1")
	field := Field("mode", func(value *struct{ Mode int }) *int { return &value.Mode }, Int().Help("mode"), Alias("m"))
	schema := builder.AddField(field).Build()
	description := schema.Description()
	if len(description.Fields) != 1 {
		t.Fatalf("description fields = %#v, schema error = %v", description.Fields, schema.Err())
	}
	if len(description.Fields[0].Aliases) != 1 {
		t.Fatalf("description aliases = %#v, schema error = %v", description.Fields[0].Aliases, schema.Err())
	}
	description.Fields[0].Aliases[0] = "changed"
	description.Fields[0].Choices = append(description.Fields[0].Choices, ChoiceDescription{ID: "x"})
	if got := schema.Description().Fields[0].Aliases[0]; got != "m" {
		t.Fatalf("description alias mutated schema: %q", got)
	}
	if !reflect.DeepEqual(schema.Description().Fields[0].Choices, []ChoiceDescription(nil)) {
		t.Fatalf("description choices unexpectedly changed")
	}
}

type describeMarker struct{}
