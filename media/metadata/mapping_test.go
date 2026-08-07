package metadata

import (
	"strings"
	"testing"

	"github.com/godexture/godec/plugin"
)

func moodToGenre(value string) (string, bool) {
	switch value {
	case "melancholic":
		return "Blues", true
	}
	return "", false
}

func TestMappingDeclaresDirectionLossinessAndPriority(t *testing.T) {
	mapping := Map(mood, genre, Ambiguous, 10, moodToGenre)
	if !mapping.Valid() {
		t.Fatalf("mapping problem = %v", mapping.Problem())
	}
	if mapping.Source() != mood.ID() || mapping.Target() != genre.ID() {
		t.Fatalf("mapping direction = %s -> %s", mapping.Source(), mapping.Target())
	}
	if mapping.Lossiness() != Ambiguous || mapping.Priority() != 10 {
		t.Fatalf("mapping = %v, %d", mapping.Lossiness(), mapping.Priority())
	}
	value, ok := mapping.Convert("melancholic")
	if !ok || value.(string) != "Blues" {
		t.Fatalf("conversion = %v, %v", value, ok)
	}
}

func TestMappingDeclinesRatherThanGuessing(t *testing.T) {
	mapping := Map(mood, genre, Ambiguous, 0, moodToGenre)
	// A value the conversion does not recognise is declined, so the caller
	// reports a loss instead of inventing a target value.
	if _, ok := mapping.Convert("unmapped"); ok {
		t.Fatal("unrecognised value was converted")
	}
	// A value of the wrong type never reaches the typed conversion.
	if _, ok := mapping.Convert(42); ok {
		t.Fatal("value of the wrong type was converted")
	}
}

func TestMappingWithoutADeclaredContractIsRejected(t *testing.T) {
	if Map(mood, genre, Ambiguous, 0, nil).Valid() {
		t.Fatal("mapping without a conversion accepted")
	}
	if Map(mood, genre, Lossiness(0), 0, moodToGenre).Valid() {
		t.Fatal("mapping without declared lossiness accepted")
	}
	if Map(mood, mood, Lossless, 0, func(value string) (string, bool) { return value, true }).Valid() {
		t.Fatal("mapping from a key to itself accepted")
	}
	problem := Map(mood, genre, Lossiness(0), 0, moodToGenre).Problem()
	if problem == nil || !strings.Contains(problem.Error(), "lossiness") {
		t.Fatalf("problem = %v", problem)
	}
}

func TestMappingOrderIsTotalAndIndependentOfDeclarationOrder(t *testing.T) {
	high := Map(mood, genre, Ambiguous, 10, moodToGenre)
	low := Map(mood, genre, Lossless, 1, moodToGenre)
	if !high.Better(low) || low.Better(high) {
		t.Fatal("priority did not decide")
	}
	lossless := Map(artist, genre, Lossless, 5, func(value string) (string, bool) { return value, true })
	ambiguous := Map(mood, genre, Ambiguous, 5, moodToGenre)
	if !lossless.Better(ambiguous) || ambiguous.Better(lossless) {
		t.Fatal("lossiness did not break the priority tie")
	}
	first := Map(artist, genre, Lossless, 5, func(value string) (string, bool) { return value, true })
	second := Map(title, genre, Lossless, 5, func(value string) (string, bool) { return value, true })
	if first.Better(second) == second.Better(first) {
		t.Fatal("equally ranked mappings have no stable order")
	}
}

func TestBindingNamesTheEncodingThatInterpretsACarrier(t *testing.T) {
	binding := Bind(testCarrier, encodingIdentity())
	if !binding.Valid() {
		t.Fatalf("binding = %v", binding)
	}
	if targets := binding.Targets(); len(targets) != 1 || targets[0] != encodingIdentity() {
		t.Fatalf("binding targets = %v", targets)
	}
	if binding.Key() != BindingKey(testCarrier) {
		t.Fatalf("binding key = %v, want %v", binding.Key(), BindingKey(testCarrier))
	}
	// A codec binding and a metadata binding that happen to share a key string
	// live in different namespaces, so they never collide.
	if binding.Key().Namespace() == plugin.Declare[struct{ other int }]("wave.id3").Key().Namespace() {
		t.Fatal("metadata bindings share a namespace with unrelated declarations")
	}
}

func TestConflictingBindingsForOneCarrierKeepTheirDistinctTargets(t *testing.T) {
	first := Bind(testCarrier, encodingIdentity())
	second := Bind(testCarrier, otherEncodingIdentity())
	if first.Key() != second.Key() {
		t.Fatal("same carrier produced different declaration keys")
	}
	// Detection belongs to host construction; the declaration only has to keep
	// the difference visible instead of applying last-wins.
	if first.SameTargets(second) {
		t.Fatal("conflicting bindings reported the same target")
	}
}
