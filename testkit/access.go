package testkit

import (
	"fmt"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/plugin"
)

// AccessSubject identifies one source or sink Provider component inside its
// complete composition.
type AccessSubject struct {
	set      plugin.Set
	identity plugin.Identity
	coverage *Coverage
}

// AccessOf describes an Access component whose definition composes alone.
func AccessOf(definition plugin.Definition, identity plugin.Identity) AccessSubject {
	return AccessIn(plugin.NewSet(definition), identity)
}

// AccessIn describes an Access component in the complete Set it needs.
func AccessIn(set plugin.Set, identity plugin.Identity) AccessSubject {
	return AccessSubject{set: set, identity: identity}
}

// TrackAccess returns an otherwise identical subject whose completed cases
// are recorded in coverage.
func TrackAccess(subject AccessSubject, coverage *Coverage) AccessSubject {
	subject.coverage = coverage
	return subject
}

// Identity returns the marker-derived Provider component identity.
func (s AccessSubject) Identity() plugin.Identity { return s.identity }

// AccessCase contains only external storage input and its expected bytes and
// capability selection. Host, Job, Plan, transaction, and cleanup plumbing
// remain testkit-owned.
type AccessCase struct {
	Name  string
	Input AccessFixture
	Want  AccessExpectation
}

// AccessExpectation describes the byte image and ordered capability
// alternatives expected at the selected Provider boundary.
type AccessExpectation struct {
	bytes        []byte
	requirements access.Requirements
	set          bool
}

// WantAccess compares the source stream or committed sink image and requires
// Host to select the first supported alternative in the supplied order.
func WantAccess(bytes []byte, alternatives ...access.Alternative) AccessExpectation {
	return AccessExpectation{
		bytes:        append([]byte(nil), bytes...),
		requirements: access.NewRequirements(alternatives...),
		set:          true,
	}
}

// Access runs Provider capability selection, transaction, ownership, and
// cleanup contracts through the same Plan/cancel/success runner as executable
// component cases. The subject must carry exactly one source or sink trait.
func Access(t testing.TB, subject AccessSubject, cases ...AccessCase) {
	t.Helper()
	direction, err := accessDirection(subject)
	if err != nil {
		t.Fatalf("testkit Access subject: %v", err)
	}
	if len(cases) == 0 {
		t.Fatalf("testkit Access requires at least one case")
	}
	runNamed(t, "Own-Borrow", verifyAccessOwnership)
	for index := range cases {
		current := cases[index]
		name := current.Name
		if name == "" {
			name = fmt.Sprintf("case-%d", index+1)
		}
		runNamed(t, name, func(child testing.TB) {
			defer func() {
				if err := current.Input.close(); err != nil {
					child.Errorf("testkit Access fixture cleanup: %v", err)
				}
			}()
			if !current.Input.valid() {
				child.Fatalf("testkit Access input fixture is invalid")
			}
			if !current.Want.valid(direction) {
				child.Fatalf("testkit Access expectation is invalid for %s", directionName(direction))
			}
			factory := func() (*scenarioCore, error) {
				return newAccessScenario(subject, direction, current.Input, current.Want)
			}
			executeCase(child, subject.identity, "", factory)
			subject.coverage.record(subject.identity)
		})
	}
}

func accessDirection(subject AccessSubject) (access.Direction, error) {
	if subject.set.Empty() || subject.identity.IsZero() {
		return 0, fmt.Errorf("composition or identity is empty")
	}
	component, ok := componentOf(subject.set, subject.identity)
	if !ok {
		return 0, fmt.Errorf("component %s is absent from its Set", subject.identity)
	}
	source, hasSource := access.SourceOf(component)
	sink, hasSink := access.SinkOf(component)
	if hasSource == hasSink || hasSource && !source.Valid() || hasSink && !sink.Valid() {
		return 0, fmt.Errorf("component %s must carry exactly one valid source or sink trait", subject.identity)
	}
	if hasSource {
		return access.SourceDirection, nil
	}
	return access.SinkDirection, nil
}

func (e AccessExpectation) valid(direction access.Direction) bool {
	return e.set && e.requirements.ValidFor(direction)
}

func directionName(direction access.Direction) string {
	if direction == access.SourceDirection {
		return "source"
	}
	if direction == access.SinkDirection {
		return "sink"
	}
	return "unknown"
}
