package host

import (
	"context"
	"errors"
	"fmt"
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
	snapshot access.Snapshot
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
		selection, selectionErr := providerSelection(projection)
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
		var declared access.Capabilities
		accessDirection := access.SourceDirection
		class := access.TransactionClass(0)
		if projection.Direction == plan.InputBoundary {
			declared = entry.SourceTrait().Capabilities()
		} else {
			declared = entry.SinkTrait().Capabilities()
			accessDirection = access.SinkDirection
			class = entry.SinkTrait().TransactionClass()
		}
		if !capabilitySubset(declared, actual) {
			return sessions, sessionDiagnostic("prepare.access-capabilities", projection, "Access session does not provide every capability guaranteed by its component trait", map[string]string{
				"declared": capabilityNames(declared.Values()),
				"actual":   capabilityNames(actual.Values()),
				"selected": capabilityNames(selection.Capabilities()),
			})
		}
		opening, openingErr := access.NewOpening(accessDirection, session, selection, class)
		if openingErr != nil {
			return sessions, sessionDiagnostic("prepare.access-view", projection, "Access session cannot provide the selected narrow operation view", map[string]string{
				"actual":   capabilityNames(actual.Values()),
				"selected": capabilityNames(selection.Capabilities()),
				"error":    openingErr.Error(),
			})
		}
		if projection.Spool.Valid() {
			spooled, spoolErr := newSpoolSession(projection.Spool, session, opening)
			if spoolErr != nil {
				return sessions, sessionDiagnostic("prepare.spool", projection, "Access output spool could not be created", map[string]string{"error": spoolErr.Error()})
			}
			sessions[len(sessions)-1].value = spooled
			session = spooled
			effective, effectiveErr := access.NewCapabilities(projection.Effective...)
			if effectiveErr != nil || !capabilitySubset(effective, spooled.Capabilities()) {
				return sessions, sessionDiagnostic("prepare.spool-capabilities", projection, "Access output spool does not provide every planned effective capability", nil)
			}
			selection, selectionErr = boundarySelection(projection)
			if selectionErr != nil {
				return sessions, selectionErr
			}
			opening, openingErr = access.NewOpening(accessDirection, session, selection, class)
			if openingErr != nil {
				return sessions, sessionDiagnostic("prepare.spool-view", projection, "Access output spool cannot provide the selected effective view", map[string]string{"error": openingErr.Error()})
			}
			sessions[len(sessions)-1].actual = spooled.Capabilities()
			sessions[len(sessions)-1].selected = selection
		}
		sessions[len(sessions)-1].opening = opening
		if projection.Direction == plan.InputBoundary {
			snapshot, snapshotErr := readSnapshot(ctx, session)
			if snapshotErr != nil {
				return sessions, sessionDiagnostic("prepare.access-snapshot", projection, "Access source session could not report its content identity", map[string]string{"error": snapshotErr.Error()})
			}
			sessions[len(sessions)-1].snapshot = snapshot
		}
	}
	return sessions, nil
}

func readSnapshot(ctx context.Context, session access.Session) (access.Snapshot, error) {
	reporter, ok := access.SnapshotOf(session)
	if !ok {
		return access.Snapshot{}, nil
	}
	snapshot, err := reporter.Snapshot(ctx)
	if errors.Is(err, access.ErrNoSnapshot) {
		return access.Snapshot{}, nil
	}
	if err != nil {
		return access.Snapshot{}, err
	}
	if !snapshot.Valid() {
		return access.Snapshot{}, access.ErrInvalidSnapshot
	}
	return snapshot, nil
}

// verifySnapshots re-reads the content identity of every source that reports
// one. Probe, Inspect, and Compile turned the acquired bytes into planning
// facts, so a source that has changed underneath must end the job instead of
// executing a Plan that describes different content.
func verifySnapshots(ctx context.Context, phase Phase, sessions []acquiredSession) *Failure {
	if ctx == nil {
		ctx = context.Background()
	}
	for _, session := range sessions {
		if !session.snapshot.Valid() {
			continue
		}
		current, err := readSnapshot(ctx, session.value)
		if err != nil {
			failure := failureOf(phase, session.node, "access/snapshot", err)
			return &failure
		}
		// A source that identified its content at acquire has to keep doing
		// so. Losing the identity, or weakening it, leaves nothing to compare
		// and must not read as agreement.
		if current.Nature() != session.snapshot.Nature() || current.Identity() != session.snapshot.Identity() {
			failure := failureOf(phase, session.node, "access/snapshot", fmt.Errorf("source content changed after planning: %s became %s", describeSnapshot(session.snapshot), describeSnapshot(current)))
			return &failure
		}
	}
	return nil
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
	available, err := access.NewCapabilities(projection.Effective...)
	if err != nil {
		return access.Selection{}, err
	}
	selection, ok := access.Select(available, access.NewRequirements(access.AllOf(projection.Selected...)))
	if !ok {
		return access.Selection{}, access.ErrInvalidCapabilities
	}
	return selection, nil
}

func providerSelection(projection plan.Boundary) (access.Selection, error) {
	if !projection.Spool.Valid() {
		return boundarySelection(projection)
	}
	available, err := access.NewCapabilities(projection.Available...)
	if err != nil {
		return access.Selection{}, err
	}
	selection, ok := access.Select(available, access.NewRequirements(access.AllOf(access.SequentialWrite)))
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

func describeSnapshot(snapshot access.Snapshot) string {
	if !snapshot.Valid() {
		return "no content identity"
	}
	return snapshot.Identity()
}
