package manifest

import (
	"slices"
	"testing"
)

func TestIntConstraintCandidatesRespectCombinedBounds(t *testing.T) {
	t.Parallel()
	constraint := IntConstraint{Values: []int{8, 16, 24, 32}, Min: 16, Max: 24}
	if got, want := constraint.Candidates(0), []int{16, 24}; !slices.Equal(got, want) {
		t.Fatalf("Candidates() = %v, want %v", got, want)
	}
}

func TestIntConstraintCandidatesClampsToTheViolatedBound(t *testing.T) {
	t.Parallel()
	constraint := IntConstraint{Min: 1, Max: 1048575}
	if got, want := constraint.Candidates(4410000), []int{1048575}; !slices.Equal(got, want) {
		t.Fatalf("Candidates() = %v, want %v (value exceeds Max, must clamp up to Max, not down to Min)", got, want)
	}
	if got, want := constraint.Candidates(0), []int{1}; !slices.Equal(got, want) {
		t.Fatalf("Candidates() = %v, want %v (value is below Min, must clamp up to Min)", got, want)
	}
}
