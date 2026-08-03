package diagnostic

import (
	"fmt"
	"sort"
	"strings"
)

// maxSuggestions bounds how many alternatives an "unknown X" diagnostic
// offers. Listing every registered name turns a hint into noise.
const maxSuggestions = 3

// Suggest returns the candidates closest to input, nearest first. It exists so
// every "unknown field/preset/identity" diagnostic can name the alternatives
// the caller most likely meant instead of only stating that the input was
// rejected. Matching is case-insensitive and the result order is deterministic.
func Suggest(input string, candidates []string) []string {
	if input == "" || len(candidates) == 0 {
		return nil
	}
	lowered := strings.ToLower(input)
	limit := len(lowered) / 3
	if limit < 1 {
		limit = 1
	}

	type scored struct {
		candidate string
		distance  int
	}
	var matches []scored
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" || candidate == input {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}

		other := strings.ToLower(candidate)
		distance := editDistance(lowered, other)
		// A prefix relation survives an arbitrary number of edits, so accept it
		// regardless of distance: "fastest" should still suggest "fast".
		if distance <= limit || strings.HasPrefix(other, lowered) || strings.HasPrefix(lowered, other) {
			matches = append(matches, scored{candidate: candidate, distance: distance})
		}
	}
	sort.Slice(matches, func(left, right int) bool {
		if matches[left].distance != matches[right].distance {
			return matches[left].distance < matches[right].distance
		}
		return matches[left].candidate < matches[right].candidate
	})
	if len(matches) > maxSuggestions {
		matches = matches[:maxSuggestions]
	}
	result := make([]string, len(matches))
	for index, match := range matches {
		result[index] = match.candidate
	}
	return result
}

// WithSuggestions returns a copy of i carrying alternatives both in the
// human-facing message and in Detail, so a terminal reader and a machine
// consumer see the same hint.
func (i Item) WithSuggestions(suggestions []string) Item {
	if len(suggestions) == 0 {
		return i.WithPath(i.Path)
	}
	result := i.WithPath(i.Path)
	quoted := make([]string, len(suggestions))
	for index, suggestion := range suggestions {
		quoted[index] = fmt.Sprintf("%q", suggestion)
	}
	joined := strings.Join(quoted, ", ")
	result.Message = strings.TrimSuffix(result.Message, ".") + "; did you mean " + joined + "?"
	if result.Detail == nil {
		result.Detail = make(map[string]string, 1)
	}
	result.Detail["suggestions"] = strings.Join(suggestions, ",")
	return result
}

func editDistance(left, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	if len(leftRunes) == 0 {
		return len(rightRunes)
	}
	if len(rightRunes) == 0 {
		return len(leftRunes)
	}

	previous := make([]int, len(rightRunes)+1)
	current := make([]int, len(rightRunes)+1)
	for index := range previous {
		previous[index] = index
	}
	for leftIndex := 1; leftIndex <= len(leftRunes); leftIndex++ {
		current[0] = leftIndex
		for rightIndex := 1; rightIndex <= len(rightRunes); rightIndex++ {
			cost := 1
			if leftRunes[leftIndex-1] == rightRunes[rightIndex-1] {
				cost = 0
			}
			current[rightIndex] = minimum(
				previous[rightIndex]+1,
				current[rightIndex-1]+1,
				previous[rightIndex-1]+cost,
			)
		}
		previous, current = current, previous
	}
	return previous[len(rightRunes)]
}

func minimum(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}
