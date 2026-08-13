package host

import (
	"context"
	"errors"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/bind"
	"github.com/godexture/godec/internal/bound"
	internalplanning "github.com/godexture/godec/internal/planning"
	"github.com/godexture/godec/internal/program"
	"github.com/godexture/godec/internal/solve"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
)

type inputPlan struct {
	program  program.Program
	entries  []bound.Entry
	sessions []acquiredSession
	stores   []*probeStore
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
	if err := h.validatePinnedFormatSelectors(normalized.Request(), normalized.Boundaries().Entries()); err != nil {
		return inputPlan{}, errors.Join(err, closeRequestDirects(request))
	}
	if err := h.validateBoundaries(ctx, normalized.Boundaries().Entries()); err != nil {
		return inputPlan{}, errors.Join(err, closeRequestDirects(request))
	}
	planningContext, cancel := internalplanning.Start(ctx, normalized.Request().Budget().Duration)
	defer cancel()
	selected := inputPlan{entries: normalized.Boundaries().Entries()}
	selected.sessions, err = acquireSessions(planningContext, selected.entries, plan.InputBoundary)
	if err != nil {
		err = planningDurationError(planningContext, normalized.Request().Budget(), "acquire", err)
		return inputPlan{}, errors.Join(err, h.closeInputPlan(selected))
	}
	selection, err := h.selectInputFormats(planningContext, normalized.Request(), selected.entries, selected.sessions)
	if err != nil {
		return inputPlan{}, errors.Join(err, h.closeInputPlan(selected))
	}
	selected.entries = selection.entries
	selected.sessions = selection.sessions
	selected.stores = selection.stores
	selection, err = h.selectOutputFormats(selection)
	if err != nil {
		return inputPlan{}, errors.Join(err, h.closeInputPlan(selected))
	}
	selected.entries = selection.entries
	contexts, inspected, err := h.inspectInputs(planningContext, selection.request, selected.entries, selected.sessions)
	if err != nil {
		err = planningDurationError(planningContext, selection.request.Budget(), "inspect", err)
		return inputPlan{}, errors.Join(err, h.closeInputPlan(selected))
	}
	selection.preselection = selection.preselection.WithInspected(inspected)
	selected.stores, err = finishProbeStores(selected.stores)
	if err != nil {
		return inputPlan{}, errors.Join(err, h.closeInputPlan(selected))
	}
	selected.program, err = solve.ResolvePrepared(planningContext, h.index, selection.request, h.platform, bound.New(selected.entries...), contexts, selection.preselection)
	if err != nil {
		return inputPlan{}, errors.Join(err, h.closeInputPlan(selected))
	}
	return selected, nil
}

func planningDurationError(ctx context.Context, budget job.Budget, phase string, err error) error {
	if !internalplanning.DurationExhausted(ctx) {
		return err
	}
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == "prepare.probe-budget" || item.Code == "solve.budget-exhausted" {
			return err
		}
	}
	return diagnostic.NewError(diagnostic.NewItem("prepare.budget-exhausted", diagnostic.ErrorSeverity, diagnostic.Path{}, "planning duration budget was exhausted", map[string]string{
		"dimension": "duration",
		"phase":     phase,
		"limit":     budget.Duration.String(),
		"cause":     err.Error(),
	}))
}

func (h *Host) closeInputPlan(selected inputPlan) error {
	cleanupContext, cancel := context.WithTimeout(context.Background(), h.cleanupTimeout)
	defer cancel()
	return errors.Join(joinFailures(closeSessions(cleanupContext, selected.sessions)), closeProbeStores(selected.stores), closeBoundDirects(selected.entries))
}
