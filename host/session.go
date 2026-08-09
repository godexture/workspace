package host

import (
	"context"
	"errors"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/plan"
)

type acquiredSession struct {
	node     string
	value    access.Session
	actual   access.Capabilities
	selected access.Selection
}

func acquireSessions(ctx context.Context, entries []bound.Entry, includeOutputs bool) (sessions []acquiredSession, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for _, entry := range entries {
		projection := entry.Projection()
		if projection.Kind != plan.ProviderBoundary || projection.Direction == plan.OutputBoundary && !includeOutputs {
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
	}
	return sessions, nil
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
