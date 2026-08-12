package config

import (
	"fmt"
	"strings"
	"testing"
)

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

	builder := Struct[testConfig](defaultTestConfig).Version("1")
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

	planned, err := schema.Resolve(NewPatch().SetText("number", "0").Planned())
	if err != nil {
		t.Fatalf("planner resolve failed: %v", err)
	}
	if source, _ := planned.Provenance.Source("number"); source != SourcePlanner {
		t.Fatalf("planner provenance = %s, want planner", source)
	}
	if planned.Fingerprint != resolved.Fingerprint {
		t.Fatal("provenance changed the config fingerprint")
	}
	fields := schema.summary(planned.Value, planned.Provenance, planned.Fingerprint).Fields()
	foundPlanner := false
	for _, field := range fields {
		if field.ID == "number" && field.Source == SourcePlanner {
			foundPlanner = true
		}
	}
	if !foundPlanner {
		t.Fatalf("planner summary = %#v", fields)
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
		builder := Struct[orderConfig](func() orderConfig {
			return orderConfig{Labels: map[string]int{"z": 26, "a": 1}, Number: 3}
		}).Version("1")
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
	schema := Struct[goldenConfig](func() goldenConfig {
		return goldenConfig{Count: 3, Labels: map[string]int{"b": 2, "a": 1}}
	}).
		Version("1").
		AddField(Field("labels", func(value *goldenConfig) *map[string]int { return &value.Labels }, Map(String(), Int()))).
		AddField(Field("count", func(value *goldenConfig) *int { return &value.Count }, Int())).
		Build()
	canonical, err := schema.Canonical(schema.Default())
	if err != nil {
		t.Fatalf("canonical failed: %v", err)
	}
	if got := fmt.Sprintf("%x", canonical); got != "676f6465632f636f6e6669672f63616e6f6e6963616c2f763100000000000000002e6769746875622e636f6d2f676f646578747572652f676f6465632f636f6e6669672e676f6c64656e436f6e6669670000000000000001310000000000000005636f756e740000000000000003696e740000000000000005696e743a3300000000000000066c6162656c73000000000000000f6d61703c737472696e672c696e743e000000000000003e6d6170000000000000000008737472696e673a610000000000000005696e743a310000000000000008737472696e673a620000000000000005696e743a32" {
		t.Fatalf("canonical digest = %s", got)
	}
	if got := hashCanonical(canonical).String(); got != "d1b87eaa4ac17774a1448612ea8b923a2f3d1a688514d78924779d7a52c7f451" {
		t.Fatalf("fingerprint digest = %s", got)
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
