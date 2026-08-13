package config

import (
	"slices"
	"testing"
)

type otherMutableID struct{}

type mutableConfig struct {
	Values []int
	Labels map[string]int
}

func mutableSchema() Schema[mutableConfig] {
	return Struct[mutableConfig](func() mutableConfig {
		return mutableConfig{Values: []int{1, 2}, Labels: map[string]int{"a": 1}}
	}).
		Version("1").
		AddField(Field("values", func(value *mutableConfig) *[]int { return &value.Values }, Slice(Int()))).
		AddField(Field("labels", func(value *mutableConfig) *map[string]int { return &value.Labels }, Map(String(), Int()))).
		Build()
}

func mutableKey(t *testing.T, field string) Key {
	t.Helper()
	key, ok := mutableSchema().Key(field)
	if !ok {
		t.Fatalf("mutable schema has no %s field", field)
	}
	return key
}

// A patch is a request, and a request must keep the meaning it had when it was
// built. Writing through the slice the caller passed to Set must not reach it.
func TestPatchSnapshotsTypedValues(t *testing.T) {
	values := []int{7, 8}
	patch := NewPatch().Set(mutableKey(t, "values"), values)
	values[0] = 99

	resolved, err := mutableSchema().Resolve(patch)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if got := resolved.Value().Values; !slices.Equal(got, []int{7, 8}) {
		t.Errorf("resolved values = %v, want [7 8]", got)
	}
}

// The reader side matters as much as the writer side: two resolutions of the
// same patch must not share a backing array.
func TestPatchHandsOutIndependentSnapshots(t *testing.T) {
	patch := NewPatch().Set(mutableKey(t, "labels"), map[string]int{"a": 1})
	schema := mutableSchema()

	first, err := schema.Resolve(patch)
	if err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}
	firstValue := first.Value()
	firstValue.Labels["a"] = 99

	second, err := schema.Resolve(patch)
	if err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	if got := second.Value().Labels["a"]; got != 1 {
		t.Errorf("second resolution saw label %d, want 1", got)
	}
	if first.Fingerprint() != second.Fingerprint() {
		t.Error("resolving the same patch twice produced different fingerprints")
	}
}

// Resolved and its type-erased view are described as immutable. Writing
// through one accessor result must not be visible through the next.
func TestResolvedValueIsIndependentPerCall(t *testing.T) {
	schema := mutableSchema()
	resolved, err := schema.Resolve(NewPatch())
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	resolved.Value().Values[0] = 99
	resolved.Value().Labels["a"] = 99
	if got := resolved.Value(); got.Values[0] != 1 || got.Labels["a"] != 1 {
		t.Errorf("resolved value = %#v, want the original snapshot", got)
	}
	resolved.Diagnostics()
	if source, _ := resolved.Provenance().Source("values"); source != SourceDefault {
		t.Errorf("provenance for values = %v, want default", source)
	}

	view, err := schema.View().Resolve(NewPatch())
	if err != nil {
		t.Fatalf("view resolve failed: %v", err)
	}
	shaped, ok := view.Value().(mutableConfig)
	if !ok {
		t.Fatalf("view value = %T, want mutableConfig", view.Value())
	}
	shaped.Values[0] = 99
	shaped.Labels["a"] = 99
	compiled, _ := view.Value().(mutableConfig)
	if compiled.Values[0] != 1 || compiled.Labels["a"] != 1 {
		t.Errorf("second phase saw %#v, want the original snapshot", compiled)
	}
}

// A key belongs to one schema. Resolving a patch built from another schema's
// key must be reported rather than silently writing a same-named field.
func TestPatchRejectsForeignKey(t *testing.T) {
	other := Struct[otherMutableID](func() mutableConfig { return mutableConfig{} }).
		Version("1").
		AddField(Field("values", func(value *mutableConfig) *[]int { return &value.Values }, Slice(Int()))).
		AddField(Field("labels", func(value *mutableConfig) *map[string]int { return &value.Labels }, Map(String(), Int()))).
		Build()
	key, ok := other.Key("values")
	if !ok {
		t.Fatal("other schema has no values field")
	}
	if _, err := mutableSchema().Resolve(NewPatch().Set(key, []int{5})); err == nil {
		t.Error("a key from another schema was accepted")
	}
}

// An invalid key and a mistyped value are retained, not panicked on, and are
// reported together with every other resolution diagnostic.
func TestPatchRetainsInvalidEntries(t *testing.T) {
	patch := NewPatch().
		Set(Key{}, 1).
		Set(mutableKey(t, "values"), "not a slice")
	if _, err := mutableSchema().Resolve(patch); err == nil {
		t.Fatal("invalid patch entries resolved without an error")
	}
}

// A codec constructed from a variadic argument must not keep the caller's
// backing array: writing to it after construction would change what a built
// schema decodes and canonicalizes.
func TestEnumCopiesCallerChoices(t *testing.T) {
	choices := []Choice[int]{{ID: "low", Value: 1}, {ID: "high", Value: 2}}
	codec := Enum(choices...)
	choices[0] = Choice[int]{ID: "replaced", Value: 9}

	decoded, err := codec.Decode("low")
	if err != nil || decoded != 1 {
		t.Errorf("decode after caller mutation = %d/%v, want 1", decoded, err)
	}
	if encoded := codec.Encode(1); encoded != "low" {
		t.Errorf("encode after caller mutation = %q, want %q", encoded, "low")
	}
}
