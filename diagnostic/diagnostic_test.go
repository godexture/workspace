package diagnostic

import (
	"strings"
	"testing"
)

func TestErrorAggregatesItemsWithoutDetailInString(t *testing.T) {
	detail := map[string]string{"source": "cli", "redacted": "secret-value"}
	errorValue := NewError(
		NewItem("config.range", ErrorSeverity, FieldPath("encoder", "compression"), "value is outside the allowed range", detail),
		NewItem("config.warning", Warning, ComponentPath("example.component"), "using a default", nil),
	)

	if got := errorValue.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}
	message := errorValue.Error()
	if !strings.Contains(message, "encoder.compression") || !strings.Contains(message, "config.range") {
		t.Fatalf("Error() = %q, missing stable location/code", message)
	}
	if strings.Contains(message, "secret-value") {
		t.Fatalf("Error() exposed diagnostic detail: %q", message)
	}

	detail["source"] = "mutated"
	items := errorValue.Items()
	if items[0].Detail["source"] != "cli" {
		t.Fatalf("diagnostic detail was not copied")
	}
	items[0].Detail["source"] = "changed through copy"
	if errorValue.Items()[0].Detail["source"] != "cli" {
		t.Fatalf("Items() exposed internal detail map")
	}
}

func TestPathPrefixCopiesFields(t *testing.T) {
	childFields := []string{"compression", "level"}
	child := FieldPath(childFields...)
	parent := ComponentPath("github.com/acme/plugin.encoder")
	joined := child.Prefix(parent)
	childFields[0] = "changed"

	if got, want := joined.String(), "github.com/acme/plugin.encoder.compression.level"; got != want {
		t.Fatalf("joined path = %q, want %q", got, want)
	}
	if joined.Fields[0] != "compression" {
		t.Fatalf("joined path shares caller fields")
	}
}

func TestAppendPreservesNonDiagnosticError(t *testing.T) {
	aggregate := new(Error)
	aggregate.Append(errSentinel{})
	if aggregate.Len() != 1 || !strings.Contains(aggregate.Error(), "sentinel") {
		t.Fatalf("aggregate = %q, want wrapped sentinel", aggregate)
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "sentinel" }
