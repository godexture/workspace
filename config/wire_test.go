package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSurfaceDecodeRejectsUnknownNestedSliceAndMapFields(t *testing.T) {
	type nestedSurface struct{ Value int }
	type sliceSurface struct{ Values []nestedSurface }
	type mapSurface struct{ Values map[string]nestedSurface }

	makeNested := func() Schema[nestedSurface] {
		return Struct[nestedSurface](func() nestedSurface { return nestedSurface{Value: 1} }).
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
		for _, item := range diagnosticItems(err) {
			if item.Path.String() == wantPath {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s diagnostic path missing: %v", name, err)
		}
	}

	nestedSchema := makeNested()
	nested := Struct[nestedMarker](func() struct{ Value nestedSurface } { return struct{ Value nestedSurface }{} }).
		Version("1").
		AddField(Field("value", func(value *struct{ Value nestedSurface }) *nestedSurface { return &value.Value }, Nested(nestedSchema))).
		Build()
	check("nested", `{"value":1,"unknown":2}`, "value.unknown", func(value string) error {
		_, err := nested.Resolve(NewPatch().SetText("value", value))
		return err
	})

	slice := Struct[sliceSurface](func() sliceSurface { return sliceSurface{} }).
		Version("1").
		AddField(Field("value", func(value *sliceSurface) *[]nestedSurface { return &value.Values }, Slice(Nested(nestedSchema)))).
		Build()
	check("slice", `[{"value":1,"unknown":2}]`, "value.0.unknown", func(value string) error {
		_, err := slice.Resolve(NewPatch().SetText("value", value))
		return err
	})

	mapSchema := Struct[mapSurface](func() mapSurface { return mapSurface{} }).
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
	nestedSchema := Struct[nestedSurface](func() nestedSurface { return nestedSurface{} }).
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

type nestedMarker struct{}
