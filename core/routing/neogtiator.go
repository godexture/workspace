// core/domain/routing/negotiator.go
package routing

import (
	"iter"
	"slices"

	"github.com/godexture/core/domain/media"
)

type Negotiator struct {
	availablePlugins []Candidate
}

func NewNegotiator(plugins iter.Seq[Candidate]) *Negotiator {
	return &Negotiator{
		availablePlugins: slices.Collect(plugins),
	}
}

type searchState struct {
	profile media.Profile
	path    []Candidate
}

func (n *Negotiator) FindPath(src media.Profile, target Candidate) ([]Candidate, error) {
	if target.Accept(src) {
		return nil, nil
	}

	queue := []searchState{{profile: src, path: nil}}

	visited := make(map[string]bool)
	visited[src.Signature()] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, plugin := range n.availablePlugins {
			if !plugin.Accept(current.profile) {
				continue
			}

			nextProfile := plugin.Transform(current.profile)
			sig := nextProfile.Signature()

			if visited[sig] {
				continue
			}

			nextPath := make([]Candidate, len(current.path)+1)
			copy(nextPath, current.path)
			nextPath[len(current.path)] = plugin

			if target.Accept(nextProfile) {
				return nextPath, nil
			}

			visited[sig] = true
			queue = append(queue, searchState{
				profile: nextProfile,
				path:    nextPath,
			})
		}
	}

	return nil, ErrNoPathFound
}

func AsCandidates[T Candidate](seq iter.Seq[T]) iter.Seq[Candidate] {
	return func(yield func(Candidate) bool) {
		for item := range seq {
			if !yield(item) {
				return
			}
		}
	}
}
