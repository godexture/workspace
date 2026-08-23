package host

import (
	"errors"
	"fmt"

	"github.com/godexture/godec/internal/program"
	"github.com/godexture/godec/internal/scratch"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type scratchLease interface {
	plugin.Scratch
	Close() error
}

type scratchClaim struct {
	node    job.NodeID
	maximum resource.Bytes
}

func openScratch(program program.Program) (map[job.NodeID]scratchLease, error) {
	if _, err := program.Scratch(); err != nil {
		return nil, err
	}
	claims, err := program.ScratchClaims()
	if err != nil {
		return nil, err
	}
	values := make([]scratchClaim, 0, len(claims))
	for _, node := range program.Nodes() {
		claim, ok := claims[node.ID()]
		if !ok {
			continue
		}
		values = append(values, scratchClaim{node: node.ID(), maximum: claim})
	}
	return openScratchClaims(values, func(maximum resource.Bytes) (scratchLease, error) { return scratch.Open(maximum) })
}

func openScratchClaims(claims []scratchClaim, open func(resource.Bytes) (scratchLease, error)) (map[job.NodeID]scratchLease, error) {
	result := make(map[job.NodeID]scratchLease, len(claims))
	opened := make([]scratchClaim, 0, len(claims))
	for _, claim := range claims {
		lease, err := open(claim.maximum)
		if err == nil && lease == nil {
			err = errors.New("scratch opener returned nil lease")
		}
		if err == nil {
			result[claim.node] = lease
			opened = append(opened, claim)
			continue
		}
		failures := []error{err}
		for index := len(opened) - 1; index >= 0; index-- {
			openedClaim := opened[index]
			if closeErr := result[openedClaim.node].Close(); closeErr != nil {
				failures = append(failures, fmt.Errorf("scratch %s cleanup: %w", openedClaim.node, closeErr))
			}
		}
		return nil, errors.Join(failures...)
	}
	return result, nil
}

func (p *Prepared) releaseScratch() []Failure {
	p.scratchReleased.Do(func() {
		var failures []Failure
		for _, node := range p.program.Nodes() {
			// Both stores of one node are released in a fixed order, so what a
			// run reports about a failing release is the same every time.
			stores := [...]struct {
				name  string
				store scratchLease
			}{{name: "scratch", store: p.scratch[node.ID()]}, {name: "temporary", store: p.temporary[node.ID()]}}
			for _, entry := range stores {
				if entry.store == nil {
					continue
				}
				if err := protectedCall(node.ID().String(), entry.name+"/close", entry.store.Close); err != nil {
					failures = append(failures, failureOf(ResourcePhase, node.ID().String(), entry.name+"/close", err))
				}
			}
		}
		p.scratchFailures = failures
	})
	result := make([]Failure, len(p.scratchFailures))
	copy(result, p.scratchFailures)
	for index := range result {
		result[index].Stack = append([]byte(nil), result[index].Stack...)
	}
	return result
}

// openTemporary opens the node-local stores that grow rather than reserve.
// They share one ceiling, so they share one budget: what bounds them is the
// running total of what they have written, not an amount set aside before the
// run began.
func openTemporary(program program.Program) (map[job.NodeID]scratchLease, error) {
	claims, err := program.TemporaryClaims()
	if err != nil {
		return nil, err
	}
	values := make([]scratchClaim, 0, len(claims))
	for _, node := range program.Nodes() {
		claim, ok := claims[node.ID()]
		if !ok {
			continue
		}
		values = append(values, scratchClaim{node: node.ID(), maximum: claim})
	}
	if len(values) == 0 {
		return map[job.NodeID]scratchLease{}, nil
	}
	budget := program.TemporaryBudget()
	return openScratchClaims(values, func(maximum resource.Bytes) (scratchLease, error) {
		return scratch.OpenGrowing(budget, maximum)
	})
}
