package host

import (
	"context"
	"errors"
	"fmt"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/cancel"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/plan"
)

// validateBoundaries rejects a job whose output would destroy another
// boundary. It runs before anything is acquired, so a sink never creates its
// temporary file for a request that cannot be committed safely.
//
// The reference fingerprint is the scheme-independent floor: it catches every
// Provider without I/O. A sink that declares an equivalence test refines it,
// because only the Provider can resolve names that differ but denote one
// object, such as a symlink or a case-insensitive path.
func (h *Host) validateBoundaries(ctx context.Context, entries []bound.Entry) error {
	outputs := make([]bound.Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.Projection().Kind == plan.ProviderBoundary && entry.Projection().Direction == plan.OutputBoundary {
			outputs = append(outputs, entry)
		}
	}
	if len(outputs) == 0 {
		return nil
	}
	for _, output := range outputs {
		target := output.Projection()
		for _, other := range entries {
			peer := other.Projection()
			if peer.Kind != plan.ProviderBoundary || peer.Node == target.Node {
				continue
			}
			// Output pairs are symmetric, so compare each once.
			if peer.Direction == plan.OutputBoundary && peer.Node > target.Node {
				continue
			}
			same, err := sameTarget(ctx, output, other)
			if err != nil {
				if normalized := cancel.Normalize(ctx, err); normalized != nil {
					return normalized
				}
				failure := boundaryConflictError("prepare.boundary-identity", target, peer, "Access Provider could not compare two boundary references", safeCallbackCause(err))
				if _, recovered := err.(*journal.PanicError); recovered {
					return errors.Join(failure, err)
				}
				return failure
			}
			if same {
				return boundaryConflictError("prepare.boundary-conflict", target, peer, conflictMessage(peer.Direction), "")
			}
		}
	}
	return nil
}

func sameTarget(ctx context.Context, output, other bound.Entry) (bool, error) {
	target, peer := output.Reference(), other.Reference()
	if target.Fingerprint() == peer.Fingerprint() {
		return true, nil
	}
	if output.Projection().Scheme != other.Projection().Scheme {
		return false, nil
	}
	var same bool
	err := protectedCall(output.Projection().Node, "access/equivalence", func() error {
		var err error
		same, err = output.SinkTrait().Equivalent(ctx, target, peer)
		return err
	})
	return same, err
}

func safeCallbackCause(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%T", err)
}

func conflictMessage(direction plan.BoundaryDirection) string {
	if direction == plan.InputBoundary {
		return "output would overwrite an input of the same job; a successful commit destroys the source"
	}
	return "two outputs of the same job identify one target"
}

func boundaryConflictError(code string, target, peer plan.Boundary, message, cause string) error {
	detail := map[string]string{
		"output":    target.Reference,
		"conflict":  peer.Reference,
		"node":      target.Node,
		"peer":      peer.Node,
		"scheme":    target.Scheme,
		"direction": "write",
	}
	if peer.Direction == plan.InputBoundary {
		detail["direction"] = "read"
	}
	if cause != "" {
		detail["cause"] = cause
	}
	return diagnostic.NewError(diagnostic.NewItem(code, diagnostic.ErrorSeverity, diagnostic.Path{Component: target.Component}, message, detail))
}
