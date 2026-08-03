package diagnostic

import (
	"reflect"
	"strings"
	"testing"
)

func TestSuggestRanksNearestFirstAndIsBounded(t *testing.T) {
	candidates := []string{"compression", "comp", "verify", "channels", "compress"}
	got := Suggest("compresion", candidates)
	if len(got) == 0 {
		t.Fatalf("no suggestion for a one-character typo: %v", got)
	}
	if got[0] != "compression" {
		t.Fatalf("nearest suggestion = %q, want %q", got[0], "compression")
	}
	if len(got) > maxSuggestions {
		t.Fatalf("suggestions are unbounded: %v", got)
	}
}

func TestSuggestAcceptsPrefixBeyondEditLimit(t *testing.T) {
	// "fastest" is three edits from "fast", past the distance limit, but a
	// caller typing it almost certainly meant the registered preset.
	if got := Suggest("fastest", []string{"fast", "balanced"}); len(got) == 0 || got[0] != "fast" {
		t.Fatalf("prefix candidate not suggested: %v", got)
	}
}

func TestSuggestIsDeterministicAndSkipsUnrelated(t *testing.T) {
	candidates := []string{"beta", "alpha", "gamma"}
	first := Suggest("alpa", candidates)
	second := Suggest("alpa", []string{"gamma", "alpha", "beta"})
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("suggestion order depends on candidate order: %v vs %v", first, second)
	}
	if got := Suggest("totally-unrelated-xyz", candidates); len(got) != 0 {
		t.Fatalf("unrelated input produced suggestions: %v", got)
	}
	if got := Suggest("alpha", candidates); len(got) != 0 {
		t.Fatalf("exact match suggested itself: %v", got)
	}
}

func TestWithSuggestionsCarriesBothRenderings(t *testing.T) {
	item := NewItem("config.unknown-field", ErrorSeverity, FieldPath("compresion"), "field is not registered", nil)
	decorated := item.WithSuggestions([]string{"compression", "comp"})
	if !strings.Contains(decorated.Message, `did you mean "compression", "comp"?`) {
		t.Fatalf("message lacks the hint: %q", decorated.Message)
	}
	if decorated.Detail["suggestions"] != "compression,comp" {
		t.Fatalf("detail lacks structured suggestions: %v", decorated.Detail)
	}
	if item.Message != "field is not registered" || item.Detail != nil {
		t.Fatalf("WithSuggestions mutated the original item: %q %v", item.Message, item.Detail)
	}
	if unchanged := item.WithSuggestions(nil); unchanged.Message != item.Message {
		t.Fatalf("empty suggestions changed the message: %q", unchanged.Message)
	}
}
