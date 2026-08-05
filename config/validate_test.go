package config

import (
	"strings"
	"testing"
)

func TestInvalidSchemaIsAggregated(t *testing.T) {
	type invalidConfig struct{ Value int }
	badCodec := NewCodec(CodecSpec[func()]{
		Decode: func(string) (func(), error) { return nil, nil },
		Encode: func(func()) string { return "function" },
		Clone:  func(value func()) func() { return value },
	})
	schema := Struct[invalidConfig](func() invalidConfig { return invalidConfig{} }).
		AddField(Field("value", func(value *invalidConfig) *int { return &value.Value }, Int(), DependsOn("missing"))).
		AddField(Field("value", func(value *invalidConfig) *int { return &value.Value }, Int())).
		AddField(Field("fn", func(value *invalidConfig) *func() { return new(func()) }, badCodec)).
		Preset("", nil).
		Preset("", func(*invalidConfig) {}).
		Build()

	if schema.Valid() {
		t.Fatal("invalid schema reported valid")
	}
	items := schema.Diagnostics()
	if len(items) < 5 {
		t.Fatalf("got %d schema diagnostics, want aggregate: %v", len(items), schema.Err())
	}
	if !strings.Contains(schema.Err().Error(), "value") || !strings.Contains(schema.Err().Error(), "fn") {
		t.Fatalf("schema error lacks field paths: %v", schema.Err())
	}
}

func TestSchemaRequiresIdentityVersionAndRegisteredFields(t *testing.T) {
	type invalidConfig struct {
		Values []int
		Count  int
	}

	missingMetadata := Struct[metadataMarker](func() struct{ Count int } { return struct{ Count int }{} }).
		AddField(Field("count", func(value *struct{ Count int }) *int { return &value.Count }, Int())).
		Build()
	if missingMetadata.Valid() {
		t.Fatal("schema without identity/version reported valid")
	}

	schema := Struct[invalidConfig](func() invalidConfig { return invalidConfig{Values: []int{1}} }).
		Version("1").
		AddField(Field("count", func(value *invalidConfig) *int { return &value.Count }, Int())).
		Build()
	if schema.Valid() {
		t.Fatal("schema with an unregistered mutable field reported valid")
	}
	items := schema.Diagnostics()
	found := false
	for _, item := range items {
		if item.Code == codeUnregisteredField && item.Path.String() == "Values" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unregistered field diagnostic missing: %v", items)
	}
}

func TestSchemaRejectsUnregisteredScalarField(t *testing.T) {
	type scalarConfig struct {
		Registered int
		Forgotten  string
	}
	schema := Struct[scalarConfig](func() scalarConfig {
		return scalarConfig{Registered: 1, Forgotten: "not registered"}
	}).
		Version("1").
		AddField(Field("registered", func(value *scalarConfig) *int { return &value.Registered }, Int())).
		Build()

	if schema.Valid() {
		t.Fatal("schema with an unregistered scalar field reported valid")
	}
	for _, item := range schema.Diagnostics() {
		if item.Code == codeUnregisteredField && item.Path.String() == "Forgotten" {
			return
		}
	}
	t.Fatalf("unregistered scalar field diagnostic missing: %v", schema.Diagnostics())
}

func TestSchemaAllowsBlankAndZeroSizeFields(t *testing.T) {
	type markerConfig struct {
		Level  int
		_      struct{}
		Marker struct{}
	}
	schema := Struct[markerConfig](func() markerConfig { return markerConfig{Level: 1} }).
		Version("1").
		AddField(Field("level", func(value *markerConfig) *int { return &value.Level }, Int())).
		Build()

	if !schema.Valid() {
		t.Fatalf("schema with blank and zero-size fields is invalid: %v", schema.Err())
	}
}

type metadataMarker struct{}
