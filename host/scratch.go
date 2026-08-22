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
			journal := p.scratch[node.ID()]
			if journal == nil {
				continue
			}
			if err := protectedCall(node.ID().String(), "scratch/close", journal.Close); err != nil {
				failures = append(failures, failureOf(ResourcePhase, node.ID().String(), "scratch/close", err))
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
