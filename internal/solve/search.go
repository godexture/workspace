package solve

import (
	"container/heap"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type step struct {
	result candidateResult
}

type state struct {
	descriptor  stream.Descriptor
	fingerprint stream.Fingerprint
	rank        rank
	path        []step
	goal        searchResult
	terminal    bool
}

type searchResult struct {
	path      []step
	config    config.ResolvedView
	hasConfig bool
	rank      rank
	// need is what the consumer asked for when it declined a candidate. A
	// bridge is configured against it, so a component that can only be
	// configured by its consumer is reachable.
	need plugin.Need[stream.Descriptor]
}

func (r searchResult) progress() bool {
	return len(r.path) != 0 || r.hasConfig
}

type stateQueue []state

func (q stateQueue) Len() int { return len(q) }
func (q stateQueue) Less(left, right int) bool {
	if q[left].rank != q[right].rank {
		return q[left].rank.less(q[right].rank)
	}
	if q[left].fingerprint != q[right].fingerprint {
		return q[left].fingerprint.String() < q[right].fingerprint.String()
	}
	// A zero-cost requested-node goal must win before this same descriptor is
	// expanded into irrelevant bridges. Fixed automatic nodes have a positive
	// terminal rank and remain comparable with every alternative path.
	return q[left].terminal && !q[right].terminal
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

func (p *planner) search(gap graph.Gap) (searchResult, rejections, error) {
	rejected := make(rejections)
	input, inputOK := gap.Input()
	_, edgeOK := gap.Edge()
	if !inputOK || !edgeOK {
		result, accepted, _, limited, err := p.resolveFixed(gap, gap.Inputs(), p.budget.SuggestionsPerNeed, rank{})
		if err != nil {
			return searchResult{}, rejected, err
		}
		if accepted {
			if limited {
				return searchResult{}, rejected, limitError{dimension: "suggestions"}
			}
			return result, rejected, nil
		}
		return searchResult{}, rejected, rejectError{code: "gap-cardinality"}
	}
	fingerprint, err := input.Fingerprint()
	if err != nil {
		return searchResult{}, rejected, rejectError{code: "descriptor"}
	}
	if p.usage.States >= p.budget.States {
		return searchResult{}, rejected, limitError{dimension: "states"}
	}
	p.usage.States++
	// need starts as the reason the gap exists and becomes whatever the
	// consumer last asked for, which is more specific than a schema mismatch.
	need := gap.Need()
	visited := make(visits)
	visited.admit(input, fingerprint, rank{})
	queue := stateQueue{{descriptor: input, fingerprint: fingerprint}}
	heap.Init(&queue)
	stateLimit := false
	suggestionLimit := false
	suggestions := 0
	for queue.Len() != 0 {
		if err := p.checkContext(); err != nil {
			return searchResult{}, rejected, err
		}
		current := heap.Pop(&queue).(state)
		if current.terminal {
			if stateLimit {
				return searchResult{}, rejected, limitError{dimension: "states"}
			}
			if suggestionLimit {
				return searchResult{}, rejected, limitError{dimension: "suggestions"}
			}
			return current.goal, rejected, nil
		}
		if !visited.current(current) {
			continue
		}
		if current.descriptor.SchemaDescriptor().Equal(gap.ExpectedDescriptor()) {
			inputs, replaced := gap.WithCandidate(current.descriptor)
			if !replaced {
				rejected.add("gap-cardinality")
			} else {
				result, accepted, suggested, limited, err := p.resolveFixed(gap, inputs, p.budget.SuggestionsPerNeed-suggestions, current.rank)
				suggestions += suggested
				if limited {
					suggestionLimit = true
				}
				if err != nil {
					if _, limited := err.(limitError); limited {
						return searchResult{}, rejected, err
					}
					if _, canceled := err.(canceledError); canceled {
						return searchResult{}, rejected, err
					}
					rejected.add(rejectionCode(err))
				} else if accepted {
					result.path = append([]step(nil), current.path...)
					heap.Push(&queue, state{fingerprint: current.fingerprint, rank: result.rank, goal: result, terminal: true})
					if result.rank == current.rank {
						continue
					}
				} else {
					if _, stated := result.need.Desired(); stated {
						need = result.need
					}
					rejected.add("need")
				}
			}
		}

		for _, candidate := range p.candidates[current.descriptor.Schema().String()] {
			if !codecCandidateMatches(p.index, candidate.component.Identity(), current.descriptor, need) {
				rejected.add("codec-tag")
				continue
			}
			remaining := p.budget.SuggestionsPerNeed - suggestions
			if remaining < 0 {
				remaining = 0
			}
			configs, suggested, limited, err := p.bridgeConfigs(candidate, current.descriptor, need, remaining)
			suggestions += suggested
			if limited {
				suggestionLimit = true
			}
			if err != nil {
				if _, limited := err.(limitError); limited {
					return searchResult{}, rejected, err
				}
				if _, canceled := err.(canceledError); canceled {
					return searchResult{}, rejected, err
				}
				rejected.add(rejectionCode(err))
				continue
			}
			for _, resolved := range configs {
				result, err := p.compileBridge(candidate, resolved, current.descriptor)
				if err != nil {
					if _, limited := err.(limitError); limited {
						return searchResult{}, rejected, err
					}
					if _, canceled := err.(canceledError); canceled {
						return searchResult{}, rejected, err
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
		return searchResult{}, rejected, limitError{dimension: "states"}
	}
	if suggestionLimit {
		return searchResult{}, rejected, limitError{dimension: "suggestions"}
	}
	return searchResult{}, rejected, rejectError{code: "no-path"}
}

// resolveFixed validates a candidate's complete downstream input sequence,
// then lets only an automatic, unconfigured downstream node infer a config.
func (p *planner) resolveFixed(gap graph.Gap, inputs flow.Descriptors[stream.Descriptor], remaining int, base rank) (searchResult, bool, int, bool, error) {
	if err := p.beforeCompile(); err != nil {
		return searchResult{}, false, 0, false, err
	}
	compilation, requirements, err := gap.Compile(gap.Config(), inputs)
	if contextErr := p.checkContext(); contextErr != nil {
		return searchResult{}, false, 0, false, contextErr
	}
	if err != nil {
		return searchResult{}, false, 0, false, err
	}
	if len(requirements) == 0 {
		if err := p.validateFixedCompilation(gap, compilation); err != nil {
			return searchResult{}, false, 0, false, err
		}
		return p.fixedResult(gap, gap.Config(), compilation, base), true, 0, false, nil
	}
	learned := searchResult{need: descriptorNeed(requirements)}
	metadata, allowed := p.nodes[gap.Node()]
	if !allowed || !metadata.inferConfig {
		return learned, false, 0, false, nil
	}
	configs, suggested, limited, err := p.fixedConfigs(gap.Component(), inputs, requirements, gap.Config(), remaining)
	if err != nil {
		return searchResult{}, false, suggested, limited, err
	}
	best := learned
	found := false
	for _, resolved := range configs {
		if err := p.beforeCompile(); err != nil {
			return searchResult{}, false, suggested, limited, err
		}
		compilation, remainingRequirements, compileErr := gap.Compile(resolved, inputs)
		if contextErr := p.checkContext(); contextErr != nil {
			return searchResult{}, false, suggested, limited, contextErr
		}
		if compileErr != nil || len(remainingRequirements) != 0 {
			continue
		}
		if err := p.validateFixedCompilation(gap, compilation); err != nil {
			continue
		}
		candidate := p.fixedResult(gap, resolved, compilation, base)
		candidate.hasConfig = true
		if !found || candidate.rank.less(best.rank) {
			best = candidate
			found = true
		}
	}
	return best, found, suggested, limited, nil
}

func (p *planner) validateFixedCompilation(gap graph.Gap, compilation plugin.Compilation) error {
	metadata, automatic := p.nodes[gap.Node()]
	if !automatic || metadata.origin != plan.Automatic {
		return nil
	}
	return validateAutomaticCompilation(gap.Component(), compilation, p.policy, p.platform)
}

func (p *planner) fixedResult(gap graph.Gap, resolved config.ResolvedView, compilation plugin.Compilation, base rank) searchResult {
	result := searchResult{config: resolved, rank: base}
	metadata, ok := p.nodes[gap.Node()]
	if ok && metadata.origin == plan.Automatic {
		result.rank = result.rank.addCompilation(gap.Component(), resolved, compilation, p.policy)
	}
	return result
}

// descriptorNeed returns the first requirement that names the stream a
// consumer wants. A consumer that only states a condition leaves the search
// with the need the gap already had.
func descriptorNeed(requirements []plugin.Requirement[stream.Descriptor]) plugin.Need[stream.Descriptor] {
	for _, requirement := range requirements {
		if _, ok := requirement.Need().Desired(); ok {
			return requirement.Need()
		}
	}
	return plugin.Need[stream.Descriptor]{}
}
