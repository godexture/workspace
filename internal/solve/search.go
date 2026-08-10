package solve

import (
	"container/heap"

	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/media/stream"
)

type step struct {
	result candidateResult
}

type state struct {
	descriptor  stream.Descriptor
	fingerprint stream.Fingerprint
	rank        rank
	path        []step
}

type stateQueue []state

func (q stateQueue) Len() int { return len(q) }
func (q stateQueue) Less(left, right int) bool {
	if q[left].rank != q[right].rank {
		return q[left].rank.less(q[right].rank)
	}
	return q[left].fingerprint.String() < q[right].fingerprint.String()
}
func (q stateQueue) Swap(left, right int) { q[left], q[right] = q[right], q[left] }
func (q *stateQueue) Push(value any)      { *q = append(*q, value.(state)) }
func (q *stateQueue) Pop() any {
	old := *q
	last := old[len(old)-1]
	*q = old[:len(old)-1]
	return last
}

type visit struct {
	descriptor stream.Descriptor
	rank       rank
}

type visits map[stream.Fingerprint][]visit

func (v visits) admit(descriptor stream.Descriptor, fingerprint stream.Fingerprint, candidate rank) (bool, bool) {
	bucket := v[fingerprint]
	for index, existing := range bucket {
		if !existing.descriptor.SameState(descriptor) {
			continue
		}
		if !candidate.less(existing.rank) {
			return false, false
		}
		bucket[index].rank = candidate
		v[fingerprint] = bucket
		return true, false
	}
	v[fingerprint] = append(bucket, visit{descriptor: descriptor, rank: candidate})
	return true, true
}

func (v visits) current(candidate state) bool {
	for _, existing := range v[candidate.fingerprint] {
		if existing.descriptor.SameState(candidate.descriptor) {
			return existing.rank == candidate.rank
		}
	}
	return false
}

func (p *planner) search(gap graph.Gap) ([]step, rejections, error) {
	rejected := make(rejections)
	input, inputOK := gap.Input()
	if _, edgeOK := gap.Edge(); !inputOK || !edgeOK {
		return nil, rejected, rejectError{code: "gap-cardinality"}
	}
	fingerprint, err := input.Fingerprint()
	if err != nil {
		return nil, rejected, rejectError{code: "descriptor"}
	}
	if p.usage.States >= p.budget.States {
		return nil, rejected, limitError{dimension: "states"}
	}
	p.usage.States++
	visited := make(visits)
	visited.admit(input, fingerprint, rank{})
	queue := stateQueue{{descriptor: input, fingerprint: fingerprint}}
	heap.Init(&queue)
	stateLimit := false
	suggestionLimit := false
	suggestions := 0

	for queue.Len() != 0 {
		if err := p.checkContext(); err != nil {
			return nil, rejected, err
		}
		current := heap.Pop(&queue).(state)
		if !visited.current(current) {
			continue
		}
		if current.descriptor.Schema() == gap.ExpectedSchema() {
			if err := p.beforeCompile(); err != nil {
				return nil, rejected, err
			}
			accepted, err := gap.Accepts(current.descriptor)
			if err == nil && accepted {
				if stateLimit {
					return nil, rejected, limitError{dimension: "states"}
				}
				if suggestionLimit {
					return nil, rejected, limitError{dimension: "suggestions"}
				}
				return current.path, rejected, nil
			}
			if err != nil {
				rejected.add(rejectionCode(err))
			} else {
				rejected.add("need")
			}
		}

		for _, candidate := range p.candidates[current.descriptor.Schema().String()] {
			if !codecCandidateMatches(p.index, candidate.component.Identity(), current.descriptor) {
				rejected.add("codec-tag")
				continue
			}
			remaining := p.budget.SuggestionsPerNeed - suggestions
			if remaining < 0 {
				remaining = 0
			}
			configs, suggested, limited, err := p.configs(candidate, current.descriptor, gap.Need(), remaining)
			suggestions += suggested
			if limited {
				suggestionLimit = true
			}
			if err != nil {
				if _, limited := err.(limitError); limited {
					return nil, rejected, err
				}
				if _, canceled := err.(canceledError); canceled {
					return nil, rejected, err
				}
				rejected.add(rejectionCode(err))
				continue
			}
			for _, resolved := range configs {
				result, err := p.compileBridge(candidate, resolved, current.descriptor)
				if err != nil {
					if _, limited := err.(limitError); limited {
						return nil, rejected, err
					}
					if _, canceled := err.(canceledError); canceled {
						return nil, rejected, err
					}
					rejected.add(rejectionCode(err))
					continue
				}
				if result.output.SameState(current.descriptor) {
					rejected.add("non-progress")
					continue
				}
				outputFingerprint, err := result.output.Fingerprint()
				if err != nil {
					rejected.add("descriptor")
					continue
				}
				nextRank := current.rank.add(result, p.policy)
				admitted, unique := visited.admit(result.output, outputFingerprint, nextRank)
				if !admitted {
					continue
				}
				if unique {
					if p.usage.States >= p.budget.States {
						stateLimit = true
						continue
					}
					p.usage.States++
				}
				path := append([]step(nil), current.path...)
				path = append(path, step{result: result})
				heap.Push(&queue, state{descriptor: result.output, fingerprint: outputFingerprint, rank: nextRank, path: path})
			}
		}
	}
	if stateLimit {
		return nil, rejected, limitError{dimension: "states"}
	}
	if suggestionLimit {
		return nil, rejected, limitError{dimension: "suggestions"}
	}
	return nil, rejected, rejectError{code: "no-path"}
}
