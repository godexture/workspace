package metadata

import (
	"errors"
	"strings"
	"testing"
)

func TestThirdPartyKeysAreOpenAndDistinctWithoutCoreChanges(t *testing.T) {
	if !title.Valid() || !mood.Valid() || title.ID() == mood.ID() {
		t.Fatalf("keys = %v, %v", title.ID(), mood.ID())
	}
	if title.ID().Name() != "titleID" || !strings.HasSuffix(title.ID().PackagePath(), "media/metadata") {
		t.Fatalf("key identity = %q", title.ID())
	}
	if KeyIdentityOf[titleID]() != title.ID() {
		t.Fatal("key identity is not reproducible from its marker")
	}
}

func TestReferenceValuedKeyRequiresADeclaredClone(t *testing.T) {
	type sliceKeyID struct{}
	undeclared := DefineKey[sliceKeyID, []string]()
	if undeclared.Valid() {
		t.Fatal("reference-valued key without a clone was accepted")
	}
	if problem := undeclared.Problem(); problem == nil || !strings.Contains(problem.Error(), "clone") {
		t.Fatalf("problem = %v", problem)
	}
	declared := DefineKey[sliceKeyID, []string](func(value []string) []string {
		return append([]string(nil), value...)
	})
	if !declared.Valid() {
		t.Fatalf("declared clone rejected: %v", declared.Problem())
	}
	source := []string{"a"}
	document, err := Add(NewBuilder(AssetScope), declared, source, Origin{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	source[0] = "changed"
	if values := declared.Values(document); values[0][0] != "a" {
		t.Fatalf("stored value tracked the caller's slice: %v", values)
	}
}

func TestKeyWithAnInvalidMarkerIsReportedNotSilentlyDropped(t *testing.T) {
	invalid := DefineKey[struct{}, string]()
	if invalid.Valid() {
		t.Fatal("anonymous marker accepted")
	}
	_, err := Add(NewBuilder(AssetScope), invalid, "value", Origin{}).Build()
	if err == nil {
		t.Fatal("entry with an undeclared key accepted")
	}
}

func TestValuesIgnoreEntriesOfAnotherKey(t *testing.T) {
	builder := NewBuilder(AssetScope)
	Add(builder, title, "T", Origin{})
	Add(builder, rating, 5, Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if values := rating.Values(document); len(values) != 1 || values[0] != 5 {
		t.Fatalf("typed values = %v", values)
	}
	if _, ok := mood.First(document); ok {
		t.Fatal("absent key reported a value")
	}
}

func TestEntryValueSnapshotsThroughTheKeyClone(t *testing.T) {
	data := []byte{1, 2, 3}
	document, err := Add(NewBuilder(AssetScope), artwork, Picture{Description: "cover", Data: NewBlob("image/png", data)}, Origin{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	value, ok := document.Entries()[0].Value().(Picture)
	if !ok {
		t.Fatalf("erased value = %T", document.Entries()[0].Value())
	}
	data[0] = 9
	if got := value.Data.AppendTo(nil); got[0] != 1 {
		t.Fatalf("blob tracked the caller's slice: %v", got)
	}
}

func TestWrongValueTypeIsRejected(t *testing.T) {
	builder := NewBuilder(AssetScope)
	builder.add(title, 42, Origin{})
	_, err := builder.Build()
	if !errors.Is(err, ErrKeyType) {
		t.Fatalf("type mismatch error = %v", err)
	}
}
