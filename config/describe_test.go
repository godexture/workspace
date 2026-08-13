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

func TestSchemaViewResolvesCompleteTypedValue(t *testing.T) {
	type value struct{ Mode int }
	type marker struct{}
	schema := Struct[marker](func() value { return value{} }).
		Version("1").
		AddField(Field("mode", func(item *value) *int { return &item.Mode }, Int().Range(0, 4))).
		Build()
	view := schema.View()
	resolved, err := view.ResolveValue(value{Mode: 3})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Schema() != schema.Description().Identity || resolved.Fingerprint().IsZero() || resolved.Value().(value).Mode != 3 {
		t.Fatalf("resolved view = %#v", resolved)
	}
	if _, err := view.ResolveValue(struct{ Mode int }{Mode: 3}); err == nil {
		t.Fatal("wrong complete config type was accepted")
	}
}

func TestSchemaViewSummaryAndPatch(t *testing.T) {
	type value struct {
		Count int
		Mode  string
	}
	type marker struct{}
	schema := Struct[marker](func() value { return value{Count: 1, Mode: "fast"} }).
		Version("2").
		AddField(Field("mode", func(item *value) *string { return &item.Mode }, String())).
		AddField(Field("count", func(item *value) *int { return &item.Count }, Int())).
		Build()
	view := schema.View()
	count, ok := view.Key("count")
	if !ok {
		t.Fatal("schema view has no count field key")
	}
	resolved, err := view.Resolve(NewPatch().Set(count, 3))
	if err != nil {
		t.Fatal(err)
	}

	summary := resolved.Summary()
	if !summary.Valid() || summary.Schema() != schema.Description().Identity || summary.Version() != "2" || summary.Fingerprint() != resolved.Fingerprint() {
		t.Fatalf("summary identity = %#v", summary)
	}
	fields := summary.Fields()
	if len(fields) != 2 || fields[0].ID != "count" || fields[0].Value != "3" || fields[0].Source != SourceExplicit || fields[1].ID != "mode" || fields[1].Value != "fast" || fields[1].Source != SourceDefault {
		t.Fatalf("summary fields = %#v", fields)
	}
	fields[0].Value = "changed"
	if got := summary.Fields()[0].Value; got != "3" {
		t.Fatalf("summary fields were mutable: %q", got)
	}

	patch, err := view.Patch(resolved)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := view.Resolve(patch)
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip.Fingerprint() != resolved.Fingerprint() || !reflect.DeepEqual(roundTrip.Value(), resolved.Value()) {
		t.Fatalf("round trip = %#v, want %#v", roundTrip, resolved)
	}
}

type describeMarker struct{}
