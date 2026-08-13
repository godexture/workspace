package testkit

import (
	"strings"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/plugin"
)

type suggestConfigID struct{}

type suggestConfig struct{ Factor int }

func suggestCandidate(t *testing.T, factor int) config.ResolvedView {
	t.Helper()
	schema := config.Struct[suggestConfigID](func() suggestConfig { return suggestConfig{Factor: 1} }).
		Version("1").
		AddField(config.Field("factor", func(value *suggestConfig) *int { return &value.Factor }, config.Int().Range(1, 8))).
		Build()
	resolved, err := schema.View().ResolveValue(suggestConfig{Factor: factor})
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func suggestIdentity(t *testing.T) string {
	t.Helper()
	return suggestCandidate(t, 1).Schema()
}

// The Suggest gate is only worth having if it actually fires. Each rule is
// checked against a candidate list that breaks exactly that rule.
func TestSuggestVerificationRejectsBrokenCandidateLists(t *testing.T) {
	identity := suggestIdentity(t)
	one := suggestCandidate(t, 1)
	two := suggestCandidate(t, 2)
	view := plugin.ComponentView{HasSuggest: true, SuggestionLimit: 2}

	cases := []struct {
		name      string
		view      plugin.ComponentView
		candidate []config.ResolvedView
		repeated  []config.ResolvedView
		want      []Candidate
		problem   string
	}{
		{
			name:      "over-the-declared-limit",
			view:      plugin.ComponentView{HasSuggest: true, SuggestionLimit: 1},
			candidate: []config.ResolvedView{one, two},
			repeated:  []config.ResolvedView{one, two},
			problem:   "declared limit",
		},
		{
			name:      "not-repeatable",
			view:      view,
			candidate: []config.ResolvedView{one},
			repeated:  []config.ResolvedView{two},
			problem:   "repeatability",
		},
		{
			name:      "duplicate-canonical-config",
			view:      view,
			candidate: []config.ResolvedView{one, one},
			repeated:  []config.ResolvedView{one, one},
			problem:   "repeats canonical config",
		},
		{
			name:      "unresolved-candidate",
			view:      view,
			candidate: []config.ResolvedView{{}},
			repeated:  []config.ResolvedView{{}},
			problem:   "schema",
		},
		{
			name:      "unexpected-candidate-count",
			view:      view,
			candidate: []config.ResolvedView{one},
			repeated:  []config.ResolvedView{one},
			want:      nil,
			problem:   "want 0",
		},
		{
			name:      "unexpected-field-value",
			view:      view,
			candidate: []config.ResolvedView{one},
			repeated:  []config.ResolvedView{one},
			want:      []Candidate{{"factor": "2"}},
			problem:   `field "factor"`,
		},
		{
			name:      "unknown-field",
			view:      view,
			candidate: []config.ResolvedView{one},
			repeated:  []config.ResolvedView{one},
			want:      []Candidate{{"missing": "1"}},
			problem:   "has no field",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := verifySuggestions(testCase.view, identity, testCase.candidate, testCase.repeated, testCase.want)
			if err == nil {
				t.Fatalf("verification accepted %s", testCase.name)
			}
			if !strings.Contains(err.Error(), testCase.problem) {
				t.Fatalf("verification error = %v, want it to mention %q", err, testCase.problem)
			}
		})
	}
}

func TestSuggestVerificationAcceptsABoundedList(t *testing.T) {
	identity := suggestIdentity(t)
	candidates := []config.ResolvedView{suggestCandidate(t, 1), suggestCandidate(t, 2)}
	view := plugin.ComponentView{HasSuggest: true, SuggestionLimit: 2}
	want := []Candidate{{"factor": "1"}, {"factor": "2"}}
	if err := verifySuggestions(view, identity, candidates, candidates, want); err != nil {
		t.Fatalf("verification rejected a conforming list: %v", err)
	}
}
