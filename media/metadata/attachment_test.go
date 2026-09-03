package metadata

import (
	"errors"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plugin"
)

func attachmentDocument(t testing.TB, scope Scope) Document {
	t.Helper()
	document, err := NewBuilder(scope).Build()
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestAttachmentStatesAndSemanticAccess(t *testing.T) {
	absent := Absent()
	if !absent.Valid() || !absent.IsAbsent() || absent.State() != AttachmentAbsent || absent.Scope() != 0 {
		t.Fatalf("zero attachment = %#v", absent)
	}
	if _, err := absent.Semantic(); !errors.Is(err, ErrMetadataAbsent) {
		t.Fatalf("absent semantic error = %v", err)
	}
	document := attachmentDocument(t, StreamScope)
	available, err := NewAvailable(document)
	if err != nil {
		t.Fatal(err)
	}
	if !available.Valid() || !available.IsAvailable() || available.Scope() != StreamScope {
		t.Fatalf("available attachment = %#v", available)
	}
	semantic, err := available.Semantic()
	if err != nil || semantic.Scope() != StreamScope {
		t.Fatalf("available semantic = %#v/%v", semantic, err)
	}
	plainUnavailable, err := NewUnavailable(StreamScope)
	if err != nil {
		t.Fatal(err)
	}
	if !plainUnavailable.Valid() || !plainUnavailable.IsUnavailable() || plainUnavailable.Scope() != StreamScope {
		t.Fatalf("unavailable attachment = %#v", plainUnavailable)
	}
	if _, err := plainUnavailable.Semantic(); !errors.Is(err, ErrMetadataUnavailable) {
		t.Fatalf("unavailable semantic error = %v", err)
	}
}

func TestAttachmentConstructorsRejectInvalidScope(t *testing.T) {
	value := Attachment{state: Availability(99)}
	_, semanticErr := value.Semantic()
	if value.Valid() || !errors.Is(semanticErr, ErrInvalidAttachment) {
		t.Fatalf("invalid attachment state = %#v", value)
	}
	if _, err := NewAvailable(Document{}); !errors.Is(err, ErrInvalidAttachment) {
		t.Fatalf("invalid available document error = %v", err)
	}
	if _, err := NewUnavailable(0); !errors.Is(err, ErrInvalidAttachment) {
		t.Fatalf("invalid unavailable scope error = %v", err)
	}
}

func TestAttachmentSameStateComparesAvailabilityAndScope(t *testing.T) {
	available := MustAvailable(attachmentDocument(t, StreamScope))
	availableOther := MustAvailable(attachmentDocument(t, StreamScope))
	unavailable := MustUnavailable(StreamScope)
	assetUnavailable := MustUnavailable(AssetScope)
	if !available.SameState(availableOther) || !availableOther.SameState(available) {
		t.Fatal("equivalent available states differ")
	}
	if available.SameState(unavailable) || unavailable.SameState(assetUnavailable) {
		t.Fatal("availability or scope difference was ignored")
	}
	if Absent().SameState(available) || available.SameState(Absent()) {
		t.Fatal("absent and available states were treated as equal")
	}
	if !unavailable.SameState(MustUnavailable(StreamScope)) {
		t.Fatal("unavailable state was not stable")
	}
}

func TestAttachmentMergePreservesOrderAndUnavailableState(t *testing.T) {
	left, err := Add(NewBuilder(StreamScope), title, "left", Origin{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	right, err := Add(NewBuilder(StreamScope), title, "right", Origin{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	merged, err := Merge(StreamScope, MustAvailable(left), MustAvailable(right))
	if err != nil {
		t.Fatal(err)
	}
	document, err := merged.Semantic()
	if err != nil {
		t.Fatal(err)
	}
	if values := Values(document, title); len(values) != 2 || values[0] != "left" || values[1] != "right" {
		t.Fatalf("merged values = %v", values)
	}
	unavailable, err := Merge(StreamScope, MustUnavailable(StreamScope), MustAvailable(left))
	if err != nil {
		t.Fatal(err)
	}
	if !unavailable.IsUnavailable() {
		t.Fatalf("available metadata cleared unavailable state: %#v", unavailable)
	}
	if _, err := unavailable.Semantic(); !errors.Is(err, ErrMetadataUnavailable) {
		t.Fatalf("merged unavailable semantic error = %v", err)
	}
	if got, err := Merge(StreamScope, Absent(), MustAvailable(left)); err != nil || !got.IsAvailable() {
		t.Fatalf("absent merge = %#v/%v", got, err)
	}
	if _, err := Merge(StreamScope, MustAvailable(left), MustUnavailable(AssetScope)); !errors.Is(err, ErrMetadataScopeMismatch) {
		t.Fatalf("scope mismatch = %v", err)
	}
	if _, err := Merge(StreamScope, MustAvailable(attachmentDocument(t, AssetScope)), Absent()); !errors.Is(err, ErrMetadataScopeMismatch) {
		t.Fatalf("absent-side scope mismatch = %v", err)
	}
	if _, err := Merge(0, Absent()); !errors.Is(err, ErrInvalidAttachment) {
		t.Fatalf("invalid expected scope error = %v", err)
	}
}

func TestAttachmentMergeUnavailableSkipsConflictingDocuments(t *testing.T) {
	block := NewRawBlock("shared", testCarrier, plugin.Identity{}, NewBlob("application/octet-stream", []byte{1}))
	first, err := NewBuilder(StreamScope).AddBlock(block).Build()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewBuilder(StreamScope).AddBlock(NewRawBlock("shared", testCarrier, plugin.Identity{}, NewBlob("application/octet-stream", []byte{2}))).Build()
	if err != nil {
		t.Fatal(err)
	}
	merged, err := Merge(StreamScope, MustAvailable(first), MustAvailable(second), MustUnavailable(StreamScope))
	if err != nil {
		t.Fatal(err)
	}
	if !merged.IsUnavailable() {
		t.Fatalf("conflicting unavailable merge = %#v", merged)
	}
}

func TestResolverRejectsAbsentAndUnavailableAttachmentsWithDiagnostics(t *testing.T) {
	slot := testCarrier
	marshalCalls := 0
	component := encodingTraitComponent(
		func(ctx ParseContext) (Document, error) { return NewBuilder(ctx.Scope()).Build() },
		func(MarshalContext) (Blob, []loss.Loss, error) {
			marshalCalls++
			return NewBlob("application/octet-stream", nil), nil, nil
		},
	)
	resolver, err := NewResolver(map[carrier.ID]plugin.Component{slot: component}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		value Attachment
		code  string
		cause error
	}{
		{name: "absent", value: Absent(), code: "metadata.absent", cause: ErrMetadataAbsent},
		{name: "unavailable", value: MustUnavailable(StreamScope), code: "metadata.unavailable", cause: ErrMetadataUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := resolver.Marshal(t.Context(), slot, "block", test.value); !hasMetadataDiagnostic(err, test.code) || !errors.Is(err, test.cause) {
				t.Fatalf("attachment error = %v", err)
			}
			if _, _, err := resolver.Project(slot, "block", test.value); !hasMetadataDiagnostic(err, test.code) || !errors.Is(err, test.cause) {
				t.Fatalf("project attachment error = %v", err)
			}
			if marshalCalls != 0 {
				t.Fatalf("%s attachment invoked marshal callback %d times", test.name, marshalCalls)
			}
		})
	}
}
