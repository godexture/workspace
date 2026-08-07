package key

import (
	"strings"
	"testing"
)

type titleID struct{}

func TestDefineDerivesTypedIdentityAndErasedView(t *testing.T) {
	title := Define[titleID, string]()
	if !title.Valid() || title.ID().IsZero() {
		t.Fatalf("key = %#v, problem = %v", title, title.Problem())
	}
	if title.ID().Name() != "titleID" || !strings.HasSuffix(title.ID().PackagePath(), "media/key") {
		t.Fatalf("identity = %q", title.ID())
	}
	erased := title.Erased()
	if !erased.Valid() || erased.ID() != title.ID() || erased.ValueType() != title.ValueType() {
		t.Fatalf("erased = %#v", erased)
	}
	if value, ok := erased.Clone("value"); !ok || value != "value" {
		t.Fatalf("clone = %#v, %v", value, ok)
	}
	if _, ok := erased.Clone(1); ok {
		t.Fatal("erased clone accepted the wrong payload type")
	}
}

func TestReferenceValueRequiresDeclaredClone(t *testing.T) {
	type sliceID struct{}
	missing := Define[sliceID, []string]()
	if missing.Valid() || missing.Problem() == nil {
		t.Fatalf("undeclared clone = valid %v, problem %v", missing.Valid(), missing.Problem())
	}
	declared := Define[sliceID, []string](func(value []string) []string {
		return append([]string(nil), value...)
	})
	if !declared.Valid() {
		t.Fatalf("declared clone rejected: %v", declared.Problem())
	}
	source := []string{"before"}
	cloned, ok := declared.Erased().Clone(source)
	if !ok {
		t.Fatal("declared clone rejected a typed value")
	}
	source[0] = "after"
	if cloned.([]string)[0] != "before" {
		t.Fatalf("clone tracked caller mutation: %v", cloned)
	}
}

func TestInvalidMarkerRetainsItsProblem(t *testing.T) {
	invalid := Define[struct{}, string]()
	if invalid.Valid() || invalid.Problem() == nil {
		t.Fatalf("invalid key = valid %v, problem %v", invalid.Valid(), invalid.Problem())
	}
	if !strings.Contains(invalid.Problem().Error(), "marker") {
		t.Fatalf("problem = %v", invalid.Problem())
	}
}
