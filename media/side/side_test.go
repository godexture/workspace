package side_test

import (
	"testing"

	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/side"
)

type thirdPartySideKey struct{}

type sideValue struct{ Bytes []byte }

func cloneSideValue(value sideValue) sideValue {
	return sideValue{Bytes: append([]byte(nil), value.Bytes...)}
}

func TestThirdPartySideDataUsesMetadataCloneRules(t *testing.T) {
	key := key.Define[thirdPartySideKey, sideValue](cloneSideValue)
	original := sideValue{Bytes: []byte{1, 2, 3}}
	data, err := side.Add(side.Data{}, key, original)
	if err != nil {
		t.Fatal(err)
	}
	original.Bytes[0] = 9
	value, ok := side.First(data, key)
	if !ok || value.Bytes[0] != 1 {
		t.Fatalf("side value = %#v, %v", value, ok)
	}
	value.Bytes[1] = 8
	again, ok := side.First(data, key)
	if !ok || again.Bytes[1] != 2 {
		t.Fatalf("side snapshot = %#v, %v", again, ok)
	}
}

func TestZeroSideDataIsEmptyAndCostsNothing(t *testing.T) {
	zero := side.Data{}
	if zero.Valid() || !zero.Empty() || zero.Len() != 0 || zero.Keys() != nil {
		t.Fatal("zero side data is not empty")
	}
	key := key.Define[thirdPartySideKey, sideValue](cloneSideValue)
	if _, ok := side.First(zero, key); ok {
		t.Fatal("empty side data reported a value")
	}
}

// Adding must not change side data another item already holds.
func TestAddLeavesTheReceiverUnchanged(t *testing.T) {
	key := key.Define[thirdPartySideKey, sideValue](cloneSideValue)
	first, err := side.Add(side.Data{}, key, sideValue{Bytes: []byte{1}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := side.Add(first, key, sideValue{Bytes: []byte{2}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Len() != 1 || second.Len() != 2 {
		t.Fatalf("lengths = %d, %d", first.Len(), second.Len())
	}
	if values := side.Values(second, key); len(values) != 2 || values[0].Bytes[0] != 1 || values[1].Bytes[0] != 2 {
		t.Fatalf("insertion order = %#v", values)
	}
}

func TestUndeclaredKeyIsRejected(t *testing.T) {
	type unusableID struct{}
	undeclared := key.Define[unusableID, []string]()
	if _, err := side.Add(side.Data{}, undeclared, []string{"a"}); err == nil {
		t.Fatal("key without a declared clone accepted")
	}
}

func TestOneKeyWorksInDocumentAndSideData(t *testing.T) {
	declaration := key.Define[thirdPartySideKey, string]()
	document, err := metadata.Add(metadata.NewBuilder(metadata.StreamScope), declaration, "stream", metadata.Origin{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	data, err := side.Add(side.Data{}, declaration, "item")
	if err != nil {
		t.Fatal(err)
	}
	if values := metadata.Values(document, declaration); len(values) != 1 || values[0] != "stream" {
		t.Fatalf("document values = %v", values)
	}
	if value, ok := side.First(data, declaration); !ok || value != "item" {
		t.Fatalf("side value = %q, %v", value, ok)
	}
}
