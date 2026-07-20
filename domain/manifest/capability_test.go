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
