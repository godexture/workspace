package host

import (
	"context"
	"errors"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/bind"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/program"
	"github.com/godexture/godec/internal/solve"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
)

type inputPlan struct {
	program  program.Program
	entries  []bound.Entry
	sessions []acquiredSession
}

// Plan acquires and inspects input sessions before compilation, then closes
// every planning resource before returning. It never acquires an output
// session or reserves execution resources.
func (h *Host) Plan(ctx context.Context, request job.Job) (plan.Plan, error) {
	selected, err := h.resolveInputs(ctx, request)
	if err != nil {
		return plan.Plan{}, err
	}
	closeErr := h.closeInputPlan(selected)
	return selected.program.Plan(), closeErr
}

func (h *Host) resolveInputs(ctx context.Context, request job.Job) (inputPlan, error) {
	if h == nil {
		err := diagnostic.NewError(diagnostic.NewItem("host.nil", diagnostic.ErrorSeverity, diagnostic.Path{}, "Host is nil", nil))
		return inputPlan{}, errors.Join(err, closeRequestDirects(request))
	}
	normalized, err := bind.Normalize(h.bindings, request)
	if err != nil {
		return inputPlan{}, errors.Join(err, closeRequestDirects(request))
	}
	selected := inputPlan{entries: normalized.Boundaries().Entries()}
	selected.sessions, err = acquireSessions(ctx, selected.entries, plan.InputBoundary)
	if err != nil {
		return inputPlan{}, errors.Join(err, h.closeInputPlan(selected))
	}
	contexts, err := h.inspectInputs(ctx, normalized.Request(), selected.entries, selected.sessions)
	if err != nil {
		return inputPlan{}, errors.Join(err, h.closeInputPlan(selected))
	}
	selected.program, err = solve.ResolveBound(ctx, h.index, normalized.Request(), h.platform, normalized.Boundaries(), contexts)
	if err != nil {
		return inputPlan{}, errors.Join(err, h.closeInputPlan(selected))
	}
	return selected, nil
}

func (h *Host) closeInputPlan(selected inputPlan) error {
	cleanupContext, cancel := context.WithTimeout(context.Background(), h.cleanupTimeout)
	defer cancel()
	return errors.Join(joinFailures(closeSessions(cleanupContext, selected.sessions)), closeBoundDirects(selected.entries))
}
