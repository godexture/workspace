package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
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

func testSchema() Schema[testConfig] {
	builder := Struct(func() testConfig {
		return defaultTestConfig()
	}).Identity("test.config").Version("1")

	builder.AddField(Field("number", func(value *testConfig) *int { return &value.Number }, Int().Range(0, 10).Help("number")))
	builder.AddField(Field("verify", func(value *testConfig) *bool { return &value.Verify }, Bool()))
	builder.AddField(Field("values", func(value *testConfig) *[]int { return &value.Values }, Slice(Int())))
	builder.AddField(Field("labels", func(value *testConfig) *map[string]int { return &value.Labels }, Map(String(), Int())))
	builder.AddField(Field("nested", func(value *testConfig) *nestedConfig { return &value.Nested }, Nested(testNestedSchema())))
	builder.AddField(Field("secret", func(value *testConfig) *SecretValue[string] { return &value.Secret }, SecretCodec(String())))
	builder.AddField(Field("rate", func(value *testConfig) *Rate { return &value.Rate }, RateCodec()))
	return builder.Build()
}

func testNestedSchema() Schema[nestedConfig] {
	return Struct(func() nestedConfig { return nestedConfig{Limit: 3} }).
		Identity("test.config.nested").
		Version("1").
		AddField(Field("limit", func(value *nestedConfig) *int { return &value.Limit }, Int())).
		Build()
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
	schema := testSchema()
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
	schema := testSchema()

	_, err := schema.Resolve(NewPatch().Preset("fast"))
	if err == nil {
		// The test schema intentionally has no fast preset below; this branch
		// protects the assertion from silently testing the wrong schema.
		t.Fatal("unknown preset unexpectedly resolved")
	}

	builder := Struct(defaultTestConfig).Identity("test.config").Version("1")
	// Reuse the same field contract while adding a named preset.
	// Rebuild the typed builder explicitly so the test exercises the public
	// registration path rather than reaching into schema internals.
	builder.AddField(Field("number", func(value *testConfig) *int { return &value.Number }, Int().Range(0, 10)))
	builder.AddField(Field("verify", func(value *testConfig) *bool { return &value.Verify }, Bool()))
	builder.AddField(Field("values", func(value *testConfig) *[]int { return &value.Values }, Slice(Int())))
	builder.AddField(Field("labels", func(value *testConfig) *map[string]int { return &value.Labels }, Map(String(), Int())))
	builder.AddField(Field("nested", func(value *testConfig) *nestedConfig { return &value.Nested }, Nested(testNestedSchema())))
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
	schema := testSchema()
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

func TestCanonicalEncodingGoldenDigest(t *testing.T) {
	type goldenConfig struct {
		Count  int
		Labels map[string]int
	}
	schema := Struct(func() goldenConfig {
		return goldenConfig{Count: 3, Labels: map[string]int{"b": 2, "a": 1}}
	}).
		Identity("test.golden").
		Version("1").
		AddField(Field("labels", func(value *goldenConfig) *map[string]int { return &value.Labels }, Map(String(), Int()))).
		AddField(Field("count", func(value *goldenConfig) *int { return &value.Count }, Int())).
		Build()
	canonical, err := schema.Canonical(schema.Default())
	if err != nil {
		t.Fatalf("canonical failed: %v", err)
	}
	if got := fmt.Sprintf("%x", canonical); got != "676f6465632f636f6e6669672f63616e6f6e6963616c2f763100000000000000000b746573742e676f6c64656e0000000000000001310000000000000005636f756e740000000000000003696e740000000000000005696e743a3300000000000000066c6162656c73000000000000000f6d61703c737472696e672c696e743e000000000000003e6d6170000000000000000008737472696e673a610000000000000005696e743a310000000000000008737472696e673a620000000000000005696e743a32" {
		t.Fatalf("canonical digest = %s", got)
	}
	if got := hashCanonical(canonical).String(); got != "06e37b07f8bdefaf27b30c9ab4d8cda747f13e47760a46152db548c047baf0bf" {
		t.Fatalf("fingerprint digest = %s", got)
	}
}

func TestSecretDoesNotLeakThroughPublicRepresentations(t *testing.T) {
	schema := testSchema()
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

func TestSecretSurfaceOmitsSecretAndRejectsMarker(t *testing.T) {
	type secretConfig struct {
		Endpoint string
		Token    SecretValue[string]
	}
	schema := Struct(func() secretConfig {
		return secretConfig{Token: NewSecret("default-secret")}
	}).
		Identity("test.secret.surface").
		Version("1").
		AddField(Field("endpoint", func(value *secretConfig) *string { return &value.Endpoint }, String())).
		AddField(Field("token", func(value *secretConfig) *SecretValue[string] { return &value.Token }, SecretCodec(String()))).
		Build()
	codec := Nested(schema)

	encoded := codec.Encode(secretConfig{Endpoint: "s3://bucket", Token: NewSecret("live-secret")})
	if strings.Contains(encoded, redactionMarker) || strings.Contains(encoded, "live-secret") {
		t.Fatalf("secret appeared in wire encoding: %q", encoded)
	}
	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("secret wire decode failed: %v; encoded=%q", err, encoded)
	}
	if decoded.Endpoint != "s3://bucket" || decoded.Token.Reveal() != "default-secret" {
		t.Fatalf("secret wire decode = %#v, want endpoint and default secret", decoded)
	}

	_, err = codec.Decode(`{"endpoint":"s3://bucket","token":"<redacted>"}`)
	if err == nil {
		t.Fatal("redaction marker was accepted by nested decode")
	}
	if strings.Contains(err.Error(), redactionMarker) || strings.Contains(err.Error(), "live-secret") {
		t.Fatalf("nested decode error exposed secret data: %q", err)
	}

	_, err = schema.Resolve(NewPatch().SetText("token", redactionMarker))
	if err == nil {
		t.Fatal("redaction marker was accepted by patch decode")
	}
	items := diagnostic.ItemsOf(err)
	found := false
	for _, item := range items {
		if item.Code == codeSecretRedacted {
			found = true
			if item.Message == "field input could not be decoded" || !strings.Contains(item.Message, "redaction marker") {
				t.Fatalf("secret redaction message is not marker-specific: %#v", item)
			}
		}
		if strings.Contains(item.Message, redactionMarker) || strings.Contains(item.Message, "live-secret") {
			t.Fatalf("diagnostic message exposed secret data: %#v", item)
		}
		for _, detail := range item.Detail {
			if strings.Contains(detail, redactionMarker) || strings.Contains(detail, "live-secret") {
				t.Fatalf("diagnostic detail exposed secret data: %#v", item)
			}
		}
	}
	if !found {
		t.Fatalf("secret redaction diagnostic missing: %v", items)
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
	builder := Struct(func() struct{ Mode int } { return struct{ Mode int }{} }).Identity("test.description").Version("1")
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

func TestZeroSchemaIsInvalid(t *testing.T) {
	var schema Schema[struct{}]
	if schema.Valid() {
		t.Fatal("zero schema reported valid")
	}
	if schema.View().Valid() {
		t.Fatal("zero schema view reported valid")
	}
	if schema.Err() == nil || !strings.Contains(schema.Err().Error(), "schema has not been built") {
		t.Fatalf("zero schema error = %v", schema.Err())
	}
}

func TestSchemaRequiresIdentityVersionAndRegisteredFields(t *testing.T) {
	type invalidConfig struct {
		Values []int
		Count  int
	}

	missingMetadata := Struct(func() struct{ Count int } { return struct{ Count int }{} }).
		AddField(Field("count", func(value *struct{ Count int }) *int { return &value.Count }, Int())).
		Build()
	if missingMetadata.Valid() {
		t.Fatal("schema without identity/version reported valid")
	}

	schema := Struct(func() invalidConfig { return invalidConfig{Values: []int{1}} }).
		Identity("test.unregistered").
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
	schema := Struct(func() scalarConfig {
		return scalarConfig{Registered: 1, Forgotten: "not registered"}
	}).
		Identity("test.unregistered.scalar").
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
	schema := Struct(func() markerConfig { return markerConfig{Level: 1} }).
		Identity("test.blank-fields").
		Version("1").
		AddField(Field("level", func(value *markerConfig) *int { return &value.Level }, Int())).
		Build()

	if !schema.Valid() {
		t.Fatalf("schema with blank and zero-size fields is invalid: %v", schema.Err())
	}
}

func TestSchemaSnapshotsFactorySourceAndSecret(t *testing.T) {
	type nested struct{ Values []int }
	type shared struct {
		Exported []int
		hidden   []int
		Nested   nested
		Secret   SecretValue[[]int]
	}

	source := shared{
		Exported: []int{1},
		hidden:   []int{2},
		Nested:   nested{Values: []int{3}},
		Secret:   NewSecret([]int{4}),
	}
	nestedSchema := Struct(func() nested { return nested{Values: []int{3}} }).
		Identity("test.shared.nested").
		Version("1").
		AddField(Field("values", func(value *nested) *[]int { return &value.Values }, Slice(Int()))).
		Build()
	schema := Struct(func() shared { return source }).
		Identity("test.shared").
		Version("1").
		AddField(Field("exported", func(value *shared) *[]int { return &value.Exported }, Slice(Int()))).
		AddField(Field("hidden", func(value *shared) *[]int { return &value.hidden }, Slice(Int()))).
		AddField(Field("nested", func(value *shared) *nested { return &value.Nested }, Nested(nestedSchema))).
		AddField(Field("secret", func(value *shared) *SecretValue[[]int] { return &value.Secret }, SecretCodec(Slice(Int())))).
		Build()
	if !schema.Valid() {
		t.Fatalf("schema is invalid: %v", schema.Err())
	}

	first := schema.Default()
	first.Exported[0] = 10
	first.hidden[0] = 20
	first.Nested.Values[0] = 30
	first.Secret.Reveal()[0] = 40
	second := schema.Default()
	if second.Exported[0] != 1 || second.hidden[0] != 2 || second.Nested.Values[0] != 3 || second.Secret.Reveal()[0] != 4 {
		t.Fatalf("default snapshot shared state: %#v", second)
	}
	if source.Exported[0] != 1 || source.hidden[0] != 2 || source.Nested.Values[0] != 3 || source.Secret.Reveal()[0] != 4 {
		t.Fatalf("factory source was mutated: %#v", source)
	}

	resolved, err := schema.Resolve(NewPatch())
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	resolved.Value.Exported[0] = 99
	resolved.Value.Nested.Values[0] = 99
	next, err := schema.Resolve(NewPatch())
	if err != nil || next.Value.Exported[0] != 1 || next.Value.Nested.Values[0] != 3 {
		t.Fatalf("resolved value affected later snapshot: %#v, %v", next.Value, err)
	}
}

func TestSurfaceDecodeRejectsUnknownNestedSliceAndMapFields(t *testing.T) {
	type nestedSurface struct{ Value int }
	type sliceSurface struct{ Values []nestedSurface }
	type mapSurface struct{ Values map[string]nestedSurface }

	makeNested := func() Schema[nestedSurface] {
		return Struct(func() nestedSurface { return nestedSurface{Value: 1} }).
			Identity("test.surface.nested").
			Version("1").
			AddField(Field("value", func(value *nestedSurface) *int { return &value.Value }, Int())).
			Build()
	}
	check := func(name, value, wantPath string, resolve func(string) error) {
		t.Helper()
		err := resolve(value)
		if err == nil {
			t.Fatalf("%s unknown field unexpectedly resolved", name)
		}
		found := false
		for _, item := range diagnostic.ItemsOf(err) {
			if item.Path.String() == wantPath {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s diagnostic path missing: %v", name, err)
		}
	}

	nestedSchema := makeNested()
	nested := Struct(func() struct{ Value nestedSurface } { return struct{ Value nestedSurface }{} }).
		Identity("test.surface.direct").
		Version("1").
		AddField(Field("value", func(value *struct{ Value nestedSurface }) *nestedSurface { return &value.Value }, Nested(nestedSchema))).
		Build()
	check("nested", `{"value":1,"unknown":2}`, "value.unknown", func(value string) error {
		_, err := nested.Resolve(NewPatch().SetText("value", value))
		return err
	})

	slice := Struct(func() sliceSurface { return sliceSurface{} }).
		Identity("test.surface.slice").
		Version("1").
		AddField(Field("value", func(value *sliceSurface) *[]nestedSurface { return &value.Values }, Slice(Nested(nestedSchema)))).
		Build()
	check("slice", `[{"value":1,"unknown":2}]`, "value.0.unknown", func(value string) error {
		_, err := slice.Resolve(NewPatch().SetText("value", value))
		return err
	})

	mapSchema := Struct(func() mapSurface { return mapSurface{} }).
		Identity("test.surface.map").
		Version("1").
		AddField(Field("value", func(value *mapSurface) *map[string]nestedSurface { return &value.Values }, Map(String(), Nested(nestedSchema)))).
		Build()
	check("map", `{"item":{"value":1,"unknown":2}}`, "value.item.unknown", func(value string) error {
		_, err := mapSchema.Resolve(NewPatch().SetText("value", value))
		return err
	})
}

func TestStructuredCodecsRoundTripSurface(t *testing.T) {
	type nestedSurface struct {
		Level  int
		Tags   []string
		Labels map[string]string
	}
	nestedSchema := Struct(func() nestedSurface { return nestedSurface{} }).
		Identity("test.round-trip.nested").
		Version("1").
		AddField(Field("level", func(value *nestedSurface) *int { return &value.Level }, Int())).
		AddField(Field("labels", func(value *nestedSurface) *map[string]string { return &value.Labels }, Map(String(), String()))).
		AddField(Field("tags", func(value *nestedSurface) *[]string { return &value.Tags }, Slice(String()))).
		Build()

	assertCodecRoundTrip(t, "nested", Nested(nestedSchema), nestedSurface{
		Level:  7,
		Tags:   []string{"alpha", "two words"},
		Labels: map[string]string{"first": "one", "second": "two:two"},
	})
	assertCodecRoundTrip(t, "slice", Slice(String()), []string{"a,b", "quote\""})
	assertCodecRoundTrip(t, "map", Map(String(), String()), map[string]string{"first": "one", "second": "two:two"})
	assertCodecRoundTrip(t, "union", UnionCodec(UnionChoice[string]{ID: "text", Codec: String()}), Union[string]{Variant: "text", Value: "a:b"})
}

func assertCodecRoundTrip[T any](t *testing.T, name string, codec Codec[T], value T) {
	t.Helper()
	encoded := codec.Encode(value)
	if !json.Valid([]byte(encoded)) {
		t.Fatalf("%s Encode returned invalid JSON: %s", name, encoded)
	}
	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("%s Decode(Encode(value)) failed: %v; encoded=%s", name, err, encoded)
	}
	if !reflect.DeepEqual(decoded, value) {
		t.Fatalf("%s round trip = %#v, want %#v; encoded=%s", name, decoded, value, encoded)
	}
}

func TestIntegerDecodeChecksTargetWidth(t *testing.T) {
	if _, err := parseInt[int8]("128"); err == nil {
		t.Fatal("int8 overflow was accepted")
	}
	if _, err := parseUint[uint8]("256"); err == nil {
		t.Fatal("uint8 overflow was accepted")
	}
}

func TestMapCloneIncludesKeysAndNormalizeIncludesKeysAndValues(t *testing.T) {
	type key struct{ Value int }
	keyCodec := NewCodec(CodecSpec[*key]{
		Decode: func(value string) (*key, error) {
			var decoded int
			if _, err := fmt.Sscanf(value, "%d", &decoded); err != nil {
				return nil, err
			}
			return &key{Value: decoded}, nil
		},
		Encode: func(value *key) string { return fmt.Sprint(value.Value) },
		Canonical: func(value *key) ([]byte, error) {
			return []byte(fmt.Sprintf("key:%d", value.Value)), nil
		},
		Clone: func(value *key) *key {
			if value == nil {
				return nil
			}
			copy := *value
			return &copy
		},
	})
	codec := Map(keyCodec, Int())
	originalKey := &key{Value: 1}
	cloned := codec.Clone(map[*key]int{originalKey: 2})
	for clonedKey := range cloned {
		if clonedKey == originalKey {
			t.Fatal("map key was not cloned")
		}
		clonedKey.Value = 9
	}
	if originalKey.Value != 1 {
		t.Fatal("changing cloned key mutated original key")
	}

	normalizedKey := NewCodec(CodecSpec[int]{
		Decode: func(value string) (int, error) { return strconv.Atoi(value) },
		Encode: strconv.Itoa,
		Canonical: func(value int) ([]byte, error) {
			return []byte(fmt.Sprintf("int:%d", value)), nil
		},
		Normalize: func(value int) (int, []diagnostic.Item) { return value + 1, nil },
	})
	normalized, items := Map(normalizedKey, normalizedKey).normalizeValue(map[int]int{1: 2})
	if len(items) != 0 || normalized[2] != 3 {
		t.Fatalf("map normalize = %#v, %v", normalized, items)
	}
}
