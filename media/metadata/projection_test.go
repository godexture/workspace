package metadata

import (
	"reflect"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plugin"
)

type projectionEncodingID struct{}
type projectionConfigID struct{}

func projectionEncoding(supported ...key.Erased) (plugin.Component, *Document) {
	var marshalled Document
	schema := config.Struct[projectionConfigID](func() struct{} { return struct{}{} }).Version("1").Build()
	component := plugin.NewComponent[projectionEncodingID](plugin.Descriptor{DisplayName: "projection encoding"}, schema,
		WithEncoding(
			func(ctx ParseContext) (Document, error) { return NewBuilder(ctx.Scope()).Build() },
			func(ctx MarshalContext) (Blob, []loss.Loss, error) {
				marshalled = ctx.Document()
				return NewBlob("application/x-projection", []byte("encoded")), nil, nil
			},
			supported...,
		),
	)
	return component, &marshalled
}

func projectionResolver(t testing.TB, component plugin.Component, mappings ...Mapping) Resolver {
	t.Helper()
	resolver, err := NewResolver(map[carrier.ID]plugin.Component{testCarrier: component}, mappings)
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func projectionDocument(t testing.TB, add func(*Builder)) Document {
	t.Helper()
	builder := NewBuilder(StreamScope)
	add(builder)
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestResolverProjectPreservesDirectKeysAndConvertsOneSourceEntry(t *testing.T) {
	component, _ := projectionEncoding(title.Erased(), genre.Erased())
	mapping := Map(mood, genre, loss.Lossless, 0, func(value string) (string, bool) { return "genre:" + value, true })
	resolver := projectionResolver(t, component, mapping)
	document := projectionDocument(t, func(builder *Builder) {
		builder.AddBlock(NewSourceBlock("source", testCarrier, encodingIdentity(), NewBlob("application/octet-stream", []byte{1})))
		Add(builder, title, "direct", Origin{Encoding: encodingIdentity(), Carrier: testCarrier, Block: "source", Native: "TITLE"})
		Add(builder, mood, "calm", Origin{Encoding: encodingIdentity(), Carrier: testCarrier, Block: "source", Native: "MOOD"})
	})
	projected, reports, err := resolver.Project(testCarrier, "target", document)
	if err != nil {
		t.Fatal(err)
	}
	entries := projected.Entries()
	if len(entries) != 2 || entries[0].Key() != title.ID() || entries[0].Value() != "direct" || entries[0].Origin() != document.Entries()[0].Origin() || entries[1].Key() != genre.ID() || entries[1].Value() != "genre:calm" || entries[1].Origin() != (Origin{}) {
		t.Fatalf("projected entries = %#v", entries)
	}
	if blocks := projected.Blocks(); len(blocks) != 1 || !blocks[0].Source() {
		t.Fatalf("projected blocks = %#v", blocks)
	}
	want := loss.Report{Carrier: testCarrier, Encoding: component.Identity().String(), Block: "target", Loss: loss.Loss{
		Key: mood.ID(), Kind: loss.Converted, Target: genre.ID(), Mapping: loss.Lossless, Detail: "metadata.mapping",
		Source: loss.Origin{Carrier: testCarrier, Encoding: encodingIdentity().String(), Block: "source", Native: "MOOD"},
	}}
	if !reflect.DeepEqual(reports, []loss.Report{want}) {
		t.Fatalf("projection reports = %#v, want %#v", reports, []loss.Report{want})
	}
}

func TestResolverProjectPreservesMultipleValuesAndSelectsDeterministically(t *testing.T) {
	t.Run("multiple source entries keep order", func(t *testing.T) {
		component, _ := projectionEncoding(genre.Erased())
		resolver := projectionResolver(t, component,
			Map(mood, genre, loss.Lossless, 0, func(value string) (string, bool) { return "mood:" + value, true }),
			Map(artist, genre, loss.Lossless, 0, func(value string) (string, bool) { return "artist:" + value, true }),
		)
		document := projectionDocument(t, func(builder *Builder) {
			Add(builder, mood, "first", Origin{})
			Add(builder, artist, "second", Origin{})
		})
		projected, reports, err := resolver.Project(testCarrier, "target", document)
		if err != nil {
			t.Fatal(err)
		}
		if values := Values(projected, genre); !reflect.DeepEqual(values, []string{"mood:first", "artist:second"}) || len(reports) != 2 {
			t.Fatalf("projected values/reports = %#v/%#v", values, reports)
		}
	})
	t.Run("priority then lossiness then target", func(t *testing.T) {
		component, _ := projectionEncoding(title.Erased(), genre.Erased())
		resolver := projectionResolver(t, component,
			Map(mood, genre, loss.Lossless, 1, func(string) (string, bool) { return "low", true }),
			Map(mood, title, loss.Ambiguous, 2, func(string) (string, bool) { return "high", true }),
		)
		document := projectionDocument(t, func(builder *Builder) { Add(builder, mood, "value", Origin{}) })
		projected, reports, err := resolver.Project(testCarrier, "target", document)
		if err != nil {
			t.Fatal(err)
		}
		if value, ok := First(projected, title); !ok || value != "high" || len(reports) != 1 || reports[0].Loss.Mapping != loss.Ambiguous {
			t.Fatalf("priority projection = %#v/%#v", projected.Entries(), reports)
		}

		resolver = projectionResolver(t, component,
			Map(mood, genre, loss.Approximate, 3, func(string) (string, bool) { return "approximate", true }),
			Map(mood, title, loss.Lossless, 3, func(string) (string, bool) { return "lossless", true }),
		)
		projected, reports, err = resolver.Project(testCarrier, "target", document)
		if err != nil {
			t.Fatal(err)
		}
		if value, ok := First(projected, title); !ok || value != "lossless" || reports[0].Loss.Mapping != loss.Lossless {
			t.Fatalf("lossiness projection = %#v/%#v", projected.Entries(), reports)
		}
	})
	t.Run("decline tries the next candidate and unmapped stays", func(t *testing.T) {
		component, _ := projectionEncoding(title.Erased(), genre.Erased())
		resolver := projectionResolver(t, component,
			Map(mood, title, loss.Lossless, 2, func(string) (string, bool) { return "", false }),
			Map(mood, genre, loss.Approximate, 1, func(value string) (string, bool) { return value, true }),
		)
		document := projectionDocument(t, func(builder *Builder) {
			Add(builder, mood, "mapped", Origin{})
			Add(builder, rating, 5, Origin{})
		})
		projected, reports, err := resolver.Project(testCarrier, "target", document)
		if err != nil {
			t.Fatal(err)
		}
		entries := projected.Entries()
		if len(entries) != 2 || entries[0].Key() != genre.ID() || entries[1].Key() != rating.ID() || len(reports) != 1 || reports[0].Loss.Target != genre.ID() {
			t.Fatalf("decline/unmapped projection = %#v/%#v", entries, reports)
		}
	})
}

func TestResolverMarshalProjectsOnce(t *testing.T) {
	component, captured := projectionEncoding(artist.Erased(), genre.Erased())
	resolver := projectionResolver(t, component,
		Map(mood, artist, loss.Lossless, 0, func(value string) (string, bool) { return value, true }),
		Map(artist, genre, loss.Lossless, 0, func(value string) (string, bool) { return value, true }),
	)
	document := projectionDocument(t, func(builder *Builder) { Add(builder, mood, "once", Origin{}) })
	_, reports, err := resolver.Marshal(t.Context(), testCarrier, "target", document)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := First(*captured, artist); !ok || value != "once" || len(reports) != 1 || reports[0].Loss.Target != artist.ID() {
		t.Fatalf("Marshal projection = %#v/%#v", captured.Entries(), reports)
	}
	projected, reports, err := resolver.Project(testCarrier, "target", *captured)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := First(projected, artist); !ok || value != "once" || len(projected.Entries()) != 1 || len(reports) != 0 {
		t.Fatalf("projected document was converted twice = %#v/%#v", projected.Entries(), reports)
	}
}
