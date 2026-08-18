package testkit

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

// Candidate lists the surface field values one expected Suggest result must
// carry. Only the listed fields are compared, so a case names the decision it
// is about instead of restating a whole config. Values come from the redacted
// config summary, which keeps a secret field out of the test source.
type Candidate map[string]string

// Suggestion is one bounded Suggest scenario: ordered component inputs,
// directional descriptor demands, and the candidates the component must offer
// in order.
type Suggestion struct {
	Name    string
	Inputs  flow.Descriptors[stream.Descriptor]
	Demands []plugin.Demand[stream.Descriptor]
	Want    []Candidate
}

// Suggests verifies the bounded Suggest contract for a component that declares
// it. Suggest is the one planner-facing operation a component can implement
// without the runner ever executing it, so it needs its own entry into the
// common typed-case machinery rather than riding along with an execution case.
//
// Suggest takes no context by construction, so there is no deadline or
// cancellation for a candidate list to depend on. What remains checkable is
// checked here: the candidates match, stay inside the declared limit, are
// canonically unique, are valid for the component's own schema, and repeat.
func Suggests[I, O any](t testing.TB, subject Subject[I, O], cases ...Suggestion) {
	t.Helper()
	if !subject.valid() {
		t.Fatalf("testkit Suggest subject is invalid")
	}
	if len(cases) == 0 {
		t.Fatalf("testkit Suggest requires at least one scenario")
	}
	component, ok := componentOf(subject.set, subject.identity)
	if !ok {
		t.Fatalf("testkit Suggest subject %s is absent from its Set", subject.identity)
	}
	view := component.View()
	if !view.HasSuggest {
		t.Fatalf("testkit Suggest subject %s declares no Suggest", subject.identity)
	}
	for index := range cases {
		current := cases[index]
		name := current.Name
		if name == "" {
			name = fmt.Sprintf("suggestion-%d", index+1)
		}
		runNamed(t, name, func(child testing.TB) {
			runSuggestion(child, component, view, current)
		})
	}
	subject.coverage.recordSuggest(subject.identity)
}

func runSuggestion(t testing.TB, component plugin.Component, view plugin.ComponentView, test Suggestion) {
	t.Helper()
	for index, binding := range test.Inputs.Bindings() {
		if !binding.Valid() || !binding.Descriptor().Valid() {
			t.Fatalf("testkit Suggest input descriptor %d is invalid", index)
		}
	}
	for index, demand := range test.Demands {
		if !demand.Valid() {
			t.Fatalf("testkit Suggest demand %d is invalid", index)
		}
	}

	candidates := suggestOnce(t, component, test)
	repeated := suggestOnce(t, component, test)
	if err := verifySuggestions(view, component.Schema().Description().Identity, candidates, repeated, test.Want); err != nil {
		t.Fatal(err)
	}
}

// verifySuggestions holds every rule the Suggest contract states, separately
// from the testing.TB it is reported through, so the rules themselves can be
// tested against candidate lists that violate them.
func verifySuggestions(view plugin.ComponentView, identity string, candidates, repeated []config.ResolvedView, want []Candidate) error {
	if !equalFingerprints(candidates, repeated) {
		return fmt.Errorf("Suggest repeatability: %s != %s", fingerprintList(candidates), fingerprintList(repeated))
	}
	if len(candidates) > view.SuggestionLimit {
		return fmt.Errorf("Suggest returned %d candidates, declared limit is %d", len(candidates), view.SuggestionLimit)
	}

	seen := make(map[config.Fingerprint]struct{}, len(candidates))
	for index, candidate := range candidates {
		if candidate.Schema() != identity {
			return fmt.Errorf("candidate %d belongs to schema %q, want %q", index, candidate.Schema(), identity)
		}
		if candidate.Fingerprint().IsZero() {
			return fmt.Errorf("candidate %d has no canonical fingerprint", index)
		}
		for _, item := range candidate.Diagnostics() {
			if item.Severity == diagnostic.ErrorSeverity {
				return fmt.Errorf("candidate %d carries an error diagnostic: %s at %s", index, item.Code, item.Path)
			}
		}
		if _, duplicate := seen[candidate.Fingerprint()]; duplicate {
			return fmt.Errorf("candidate %d repeats canonical config %s", index, candidate.Fingerprint())
		}
		seen[candidate.Fingerprint()] = struct{}{}
	}

	if len(candidates) != len(want) {
		return fmt.Errorf("Suggest returned %d candidates, want %d: %s", len(candidates), len(want), summaryList(candidates))
	}
	for index, expected := range want {
		got := summaryFields(candidates[index])
		for field, value := range expected {
			actual, present := got[field]
			if !present {
				return fmt.Errorf("candidate %d has no field %q: %v", index, field, got)
			}
			if actual != value {
				return fmt.Errorf("candidate %d field %q = %q, want %q", index, field, actual, value)
			}
		}
	}
	return nil
}

func suggestOnce(t testing.TB, component plugin.Component, test Suggestion) []config.ResolvedView {
	t.Helper()
	candidates, err := plugin.Suggest(component, plugin.SuggestContext{}, plugin.NewSuggestion(test.Inputs, test.Demands...))
	if err != nil {
		t.Fatalf("Suggest failed: %v", err)
	}
	return candidates
}

func summaryFields(candidate config.ResolvedView) map[string]string {
	fields := candidate.Summary().Fields()
	result := make(map[string]string, len(fields))
	for _, field := range fields {
		result[field.ID] = field.Value
	}
	return result
}

func summaryList(candidates []config.ResolvedView) string {
	parts := make([]string, len(candidates))
	for index, candidate := range candidates {
		fields := summaryFields(candidate)
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		entries := make([]string, 0, len(keys))
		for _, key := range keys {
			entries = append(entries, key+"="+fields[key])
		}
		parts[index] = "{" + strings.Join(entries, " ") + "}"
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func equalFingerprints(left, right []config.ResolvedView) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Fingerprint() != right[index].Fingerprint() {
			return false
		}
	}
	return true
}

func fingerprintList(candidates []config.ResolvedView) string {
	parts := make([]string, len(candidates))
	for index, candidate := range candidates {
		parts[index] = candidate.Fingerprint().String()
	}
	return "[" + strings.Join(parts, " ") + "]"
}
