package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/godexture/godec/diagnostic"
)

type nestedConfig struct {
	Limit int
}

type testConfig struct {
	Number int
	Verify bool
	Values []int
	Labels map[string]int
	Nested nestedConfig
	Secret SecretValue[string]
	Rate   Rate
}

func testSchema(reverse bool) Schema[testConfig] {
	builder := Struct(func() testConfig {
		return defaultTestConfig()
	}).Identity("test.config").Version("1")

	add := func() {
		builder.AddField(Field("number", func(value *testConfig) *int { return &value.Number }, Int().Range(0, 10).Help("number")))
		builder.AddField(Field("verify", func(value *testConfig) *bool { return &value.Verify }, Bool()))
		builder.AddField(Field("values", func(value *testConfig) *[]int { return &value.Values }, Slice(Int())))
		builder.AddField(Field("labels", func(value *testConfig) *map[string]int { return &value.Labels }, Map(String(), Int())))
		builder.AddField(Field("nested", func(value *testConfig) *nestedConfig { return &value.Nested }, Nested(Struct(func() nestedConfig {
			return nestedConfig{Limit: 3}
		}).AddField(Field("limit", func(value *nestedConfig) *int { return &value.Limit }, Int())).Build())))
		builder.AddField(Field("secret", func(value *testConfig) *SecretValue[string] { return &value.Secret }, SecretCodec(String())))
		builder.AddField(Field("rate", func(value *testConfig) *Rate { return &value.Rate }, RateCodec()))
	}
	if reverse {
		add()
		return builder.Build()
	}
	add()
	return builder.Build()
}

func defaultTestConfig() testConfig {
	return testConfig{
		Number: 5,
		Values: []int{1, 2},
		Labels: map[string]int{"b": 2, "a": 1},
		Nested: nestedConfig{Limit: 3},
		Secret: NewSecret("default-token"),
		Rate:   AutoRate(),
	}
}

func TestSchemaDefaultIsFresh(t *testing.T) {
	schema := testSchema(false)
	if !schema.Valid() {
		t.Fatalf("schema is invalid: %v", schema.Err())
	}

	first := schema.Default()
	first.Values[0] = 99
	first.Labels["a"] = 99
	first.Nested.Limit = 99
	second := schema.Default()

	if second.Values[0] != 1 || second.Labels["a"] != 1 || second.Nested.Limit != 3 {
		t.Fatalf("Default() shared mutable state: %#v", second)
	}
}

func TestSchemaResolveOrderAndProvenance(t *testing.T) {
	schema := testSchema(false)

	_, err := schema.Resolve(NewPatch().Preset("fast"))
	if err == nil {
		// The test schema intentionally has no fast preset below; this branch
		// protects the assertion from silently testing the wrong schema.
		t.Fatal("unknown preset unexpectedly resolved")
	}

	builder := Struct(defaultTestConfig).Identity("test.config").Version("1")
	// Reuse the same field contract while adding a named preset.
	base := testSchema(false)
	for _, field := range base.Description().Fields {
		_ = field
	}
	// The typed builder is rebuilt explicitly so the test exercises the public
	// registration path rather than reaching into schema internals.
	builder.AddField(Field("number", func(value *testConfig) *int { return &value.Number }, Int().Range(0, 10)))
	builder.AddField(Field("verify", func(value *testConfig) *bool { return &value.Verify }, Bool()))
	builder.AddField(Field("values", func(value *testConfig) *[]int { return &value.Values }, Slice(Int())))
	builder.AddField(Field("labels", func(value *testConfig) *map[string]int { return &value.Labels }, Map(String(), Int())))
	builder.AddField(Field("nested", func(value *testConfig) *nestedConfig { return &value.Nested }, Nested(Struct(func() nestedConfig { return nestedConfig{Limit: 3} }).AddField(Field("limit", func(value *nestedConfig) *int { return &value.Limit }, Int())).Build())))
	builder.AddField(Field("secret", func(value *testConfig) *SecretValue[string] { return &value.Secret }, SecretCodec(String())))
	builder.AddField(Field("rate", func(value *testConfig) *Rate { return &value.Rate }, RateCodec()))
	builder.Preset("fast", func(value *testConfig) { value.Number = 1 })
	schema = builder.Build()
	if !schema.Valid() {
		t.Fatalf("schema is invalid: %v", schema.Err())
	}

	fromPreset, err := schema.Resolve(NewPatch().Preset("fast"))
	if err != nil {
		t.Fatalf("preset resolve failed: %v", err)
	}
	if fromPreset.Value.Number != 1 {
		t.Fatalf("preset number = %d, want 1", fromPreset.Value.Number)
	}
	if source, _ := fromPreset.Provenance.Source("number"); source != SourcePreset {
		t.Fatalf("preset provenance = %s, want preset", source)
	}

	resolved, err := schema.Resolve(NewPatch().Preset("fast").SetText("number", "0"))
	if err != nil {
		t.Fatalf("explicit resolve failed: %v", err)
	}
	if resolved.Value.Number != 0 {
		t.Fatalf("explicit zero = %d, want 0", resolved.Value.Number)
	}
	if source, _ := resolved.Provenance.Source("number"); source != SourceExplicit {
		t.Fatalf("explicit provenance = %s, want explicit", source)
	}
}

func TestSchemaAggregatesUnknownAndInvalidInput(t *testing.T) {
	schema := testSchema(false)
	_, err := schema.Resolve(NewPatch().SetText("number", "100").SetText("verify", "not-bool").SetText("missing", "1"))
	if err == nil {
		t.Fatal("invalid patch unexpectedly resolved")
	}
	items := diagnosticItems(err)
	if len(items) < 3 {
		t.Fatalf("got %d diagnostics, want at least 3: %v", len(items), err)
	}
	paths := make(map[string]bool)
	for _, item := range items {
		paths[item.Path.String()] = true
	}
	for _, path := range []string{"number", "verify", "missing"} {
		if !paths[path] {
			t.Fatalf("missing diagnostic path %q in %v", path, paths)
		}
	}
}

func TestCanonicalFingerprintIgnoresMapAndRegistrationOrder(t *testing.T) {
	type orderConfig struct {
		Labels map[string]int
		Number int
	}
	makeSchema := func(reverse bool) Schema[orderConfig] {
		builder := Struct(func() orderConfig {
			return orderConfig{Labels: map[string]int{"z": 26, "a": 1}, Number: 3}
		}).Identity("order.config").Version("1")
		if reverse {
			builder.AddField(Field("number", func(value *orderConfig) *int { return &value.Number }, Int()))
			builder.AddField(Field("labels", func(value *orderConfig) *map[string]int { return &value.Labels }, Map(String(), Int())))
		} else {
			builder.AddField(Field("labels", func(value *orderConfig) *map[string]int { return &value.Labels }, Map(String(), Int())))
			builder.AddField(Field("number", func(value *orderConfig) *int { return &value.Number }, Int()))
		}
		return builder.Build()
	}

	left, err := makeSchema(false).Resolve(NewPatch())
	if err != nil {
		t.Fatalf("left resolve failed: %v", err)
	}
	right, err := makeSchema(true).Resolve(NewPatch())
	if err != nil {
		t.Fatalf("right resolve failed: %v", err)
	}
	if left.Fingerprint != right.Fingerprint {
		t.Fatalf("fingerprints differ: %s vs %s", left.Fingerprint, right.Fingerprint)
	}

	changed := left.Value
	changed.Labels["a"] = 2
	changedResolved, err := makeSchema(false).ResolveValue(changed)
	if err != nil {
		t.Fatalf("changed resolve failed: %v", err)
	}
	if left.Fingerprint == changedResolved.Fingerprint {
		t.Fatal("fingerprint did not change with map value")
	}
}

func TestSecretDoesNotLeakThroughPublicRepresentations(t *testing.T) {
	schema := testSchema(false)
	resolved, err := schema.Resolve(NewPatch().SetText("secret", "super-secret"))
	if err != nil {
		t.Fatalf("secret resolve failed: %v", err)
	}
	if strings.Contains(fmt.Sprint(resolved.Value), "super-secret") || strings.Contains(resolved.String(), "super-secret") {
		t.Fatalf("secret leaked through public representation: value=%q resolved=%q", fmt.Sprint(resolved.Value), resolved.String())
	}
	if got := resolved.Value.Secret.Reveal(); got != "super-secret" {
		t.Fatalf("secret reveal = %q, want original value", got)
	}

	invalid := Struct(func() struct{ Secret SecretValue[int] } {
		return struct{ Secret SecretValue[int] }{Secret: NewSecret(0)}
	}).AddField(Field("secret", func(value *struct{ Secret SecretValue[int] }) *SecretValue[int] { return &value.Secret }, SecretCodec(Int().Range(1, 10)))).Build()
	_, err = invalid.Resolve(NewPatch())
	if err == nil || strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), "0") {
		t.Fatalf("secret validation error leaked a raw value: %v", err)
	}
}

func TestInvalidSchemaIsAggregated(t *testing.T) {
	type invalidConfig struct{ Value int }
	badCodec := NewCodec(CodecSpec[func()]{
		Decode: func(string) (func(), error) { return nil, nil },
		Encode: func(func()) string { return "function" },
		Clone:  func(value func()) func() { return value },
	})
	schema := Struct(func() invalidConfig { return invalidConfig{} }).
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

func TestStandardSumTypesAndCustomCodec(t *testing.T) {
	optional := OptionalCodec(Int())
	if got, err := optional.Decode("none"); err != nil || got.Present {
		t.Fatalf("optional none = %#v, %v", got, err)
	}
	auto := AutoCodec(Int())
	if got, err := auto.Decode("auto"); err != nil || got.Mode != AutoModeAuto {
		t.Fatalf("auto = %#v, %v", got, err)
	}
	if got, err := RateCodec().Decode("48000"); err != nil || got != FixedRate(48000) {
		t.Fatalf("rate = %#v, %v", got, err)
	}

	type custom int
	customCodec := NewCodec(CodecSpec[custom]{
		Decode: func(value string) (custom, error) {
			parsed, err := Int().Decode(value)
			return custom(parsed), err
		},
		Encode: func(value custom) string { return fmt.Sprintf("custom:%d", value) },
		Canonical: func(value custom) ([]byte, error) {
			return []byte(fmt.Sprintf("custom:%d", value)), nil
		},
	})
	if !customCodec.Valid() {
		t.Fatal("custom codec is invalid")
	}
}

func diagnosticItems(err error) []diagnostic.Item { return diagnostic.ItemsOf(err) }

func TestDescriptionIsImmutable(t *testing.T) {
	builder := Struct(func() struct{ Mode int } { return struct{ Mode int }{} })
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
