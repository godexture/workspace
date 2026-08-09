package host

import (
	"context"
	"errors"
	"strings"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/plan"
)

type acquiredSession struct {
	node     string
	value    access.Session
	actual   access.Capabilities
	selected access.Selection
	opening  access.Opening
}

func acquireSessions(ctx context.Context, entries []bound.Entry, direction plan.BoundaryDirection) (sessions []acquiredSession, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !direction.Valid() {
		return nil, errors.New("Access session direction is invalid")
	}
	for _, entry := range entries {
		projection := entry.Projection()
		if projection.Kind != plan.ProviderBoundary || projection.Direction != direction {
			continue
		}
		selection, selectionErr := boundarySelection(projection)
		if selectionErr != nil {
			return sessions, selectionErr
		}
		var session access.Session
		failure := invoke(ctx, PreparePhase, projection.Node, "access/acquire", func(ctx context.Context) error {
			var acquireErr error
			if projection.Direction == plan.InputBoundary {
				session, acquireErr = entry.SourceTrait().Acquire(ctx, entry.Reference(), selection)
			} else {
				session, acquireErr = entry.SinkTrait().Acquire(ctx, entry.Reference(), selection)
			}
			return acquireErr
		})
		if session != nil {
			sessions = append(sessions, acquiredSession{node: projection.Node, value: session, selected: selection})
		}
		if failure != nil {
			return sessions, *failure
		}
		if session == nil {
			return sessions, failureOf(PreparePhase, projection.Node, "access/acquire", errors.New("Access Provider acquired a nil session"))
		}
		var actual access.Capabilities
		failure = invoke(ctx, PreparePhase, projection.Node, "access/capabilities", func(context.Context) error {
			actual = session.Capabilities()
			if len(actual.Values()) == 0 || !actual.Valid() {
				return access.ErrInvalidCapabilities
			}
			return nil
		})
		if failure != nil {
			return sessions, *failure
		}
		sessions[len(sessions)-1].actual = actual
		declared := entry.SourceTrait().Capabilities()
		direction := access.SourceDirection
		class := access.TransactionClass(0)
		if projection.Direction == plan.OutputBoundary {
			declared = entry.SinkTrait().Capabilities()
			direction = access.SinkDirection
			class = entry.SinkTrait().TransactionClass()
		}
		if !capabilitySubset(declared, actual) {
			return sessions, sessionDiagnostic("prepare.access-capabilities", projection, "Access session does not provide every capability guaranteed by its component trait", map[string]string{
				"declared": capabilityNames(declared.Values()),
				"actual":   capabilityNames(actual.Values()),
				"selected": capabilityNames(selection.Capabilities()),
			})
		}
		opening, openingErr := access.NewOpening(direction, session, selection, class)
		if openingErr != nil {
			return sessions, sessionDiagnostic("prepare.access-view", projection, "Access session cannot provide the selected narrow operation view", map[string]string{
				"actual":   capabilityNames(actual.Values()),
				"selected": capabilityNames(selection.Capabilities()),
				"error":    openingErr.Error(),
			})
		}
		sessions[len(sessions)-1].opening = opening
	}
	return sessions, nil
}

func capabilitySubset(required, actual access.Capabilities) bool {
	for _, capability := range required.Values() {
		if !actual.Contains(capability) {
			return false
		}
	}
	return true
}

func capabilityNames(values []access.Capability) string {
	names := make([]string, len(values))
	for index, value := range values {
		names[index] = string(value)
	}
	return strings.Join(names, ",")
}

func sessionDiagnostic(code string, projection plan.Boundary, message string, extra map[string]string) error {
	detail := map[string]string{
		"node":      projection.Node,
		"scheme":    projection.Scheme,
		"direction": "read",
	}
	if projection.Direction == plan.OutputBoundary {
		detail["direction"] = "write"
	}
	for key, value := range extra {
		detail[key] = value
	}
	return diagnostic.NewError(diagnostic.NewItem(code, diagnostic.ErrorSeverity, diagnostic.Path{Component: projection.Component}, message, detail))
}

func boundarySelection(projection plan.Boundary) (access.Selection, error) {
	if len(projection.Selected) == 0 {
		return access.Selection{}, nil
	}
	available, err := access.NewCapabilities(projection.Available...)
	if err != nil {
		return access.Selection{}, err
	}
	selection, ok := access.Select(available, access.NewRequirements(access.AnyOf(projection.Selected...)))
	if !ok {
		return access.Selection{}, access.ErrInvalidCapabilities
	}
	return selection, nil
}

func closeSessions(ctx context.Context, sessions []acquiredSession) (failures []Failure) {
	if ctx == nil {
		ctx = context.Background()
	}
	for index := len(sessions) - 1; index >= 0; index-- {
		session := sessions[index]
		if failure := invoke(ctx, ClosePhase, session.node, "access/session", func(context.Context) error {
			return session.value.Close()
		}); failure != nil {
			failures = append(failures, *failure)
		}
	}
	return failures
}
