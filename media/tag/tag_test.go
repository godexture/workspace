package tag

import (
	"testing"

	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
)

func TestVocabularyContainsOnlyOpenDeclaredKeys(t *testing.T) {
	keys := []key.ID{
		Title.ID(), Artist.ID(), Album.ID(), Date.ID(), Genre.ID(), Comment.ID(),
		Composer.ID(), Lyrics.ID(), Website.ID(), TrackNumber.ID(), TotalTracks.ID(),
		DiscNumber.ID(), TotalDiscs.ID(), Copyright.ID(), License.ID(), Encoder.ID(),
		Picture.ID(),
	}
	seen := make(map[key.ID]struct{}, len(keys))
	for _, key := range keys {
		if key.IsZero() {
			t.Fatal("vocabulary contains an invalid key")
		}
		if _, exists := seen[key]; exists {
			t.Fatalf("vocabulary key repeated: %s", key)
		}
		seen[key] = struct{}{}
	}
	if !Date.Valid() || !Picture.Valid() {
		t.Fatalf("reference-free shared keys are invalid: date=%v picture=%v", Date.Problem(), Picture.Problem())
	}
}

func TestDeclarationsCoverVocabularyAndReturnIndependentSlices(t *testing.T) {
	first := Declarations()
	if len(first) != 17 {
		t.Fatalf("declaration count = %d, want 17", len(first))
	}
	for _, declaration := range first {
		if !declaration.Valid() {
			t.Fatalf("invalid vocabulary declaration: %v", declaration.Problem())
		}
	}
	first[0] = first[1]
	second := Declarations()
	if second[0].Key() == second[1].Key() {
		t.Fatal("Declarations returned shared mutable slice storage")
	}
}

func TestDatePreservesPartialPrecision(t *testing.T) {
	year, err := ParseDate("1985")
	if err != nil {
		t.Fatal(err)
	}
	full, err := ParseDate("1985-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if year.ToISOString() != "1985" || full.ToISOString() != "1985-01-01" {
		t.Fatalf("dates = %q, %q", year, full)
	}
	if _, ok := year.Month(); ok {
		t.Fatal("year-only date gained a month")
	}
	if month, ok := full.Month(); !ok || month != 1 {
		t.Fatalf("full date month = %d, %v", month, ok)
	}
}

func TestPictureUsesImmutableBlob(t *testing.T) {
	source := []byte{1, 2, 3}
	picture := Artwork{Data: metadata.NewBlob("image/png", source), Type: ArtworkFrontCover}
	document, err := metadata.Add(metadata.NewBuilder(metadata.AssetScope), Picture, picture, metadata.Origin{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	source[0] = 9
	value, ok := metadata.First(document, Picture)
	if !ok || !value.Valid() {
		t.Fatalf("picture = %#v, %v", value, ok)
	}
	if got := value.Data.AppendTo(nil); got[0] != 1 {
		t.Fatalf("picture payload = %v", got)
	}
}
