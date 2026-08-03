package config

import "testing"

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
