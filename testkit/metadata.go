package testkit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/plugin"
)

// MetadataSubject identifies one Metadata Encoding component inside its
// complete composition.
type MetadataSubject struct {
	set      plugin.Set
	identity plugin.Identity
	coverage *Coverage
}

// MetadataOf describes an Encoding whose definition composes alone.
func MetadataOf(definition plugin.Definition, identity plugin.Identity) MetadataSubject {
	return MetadataIn(plugin.NewSet(definition), identity)
}

// MetadataIn describes an Encoding in the complete Set it needs.
func MetadataIn(set plugin.Set, identity plugin.Identity) MetadataSubject {
	return MetadataSubject{set: set, identity: identity}
}

// TrackMetadata returns an otherwise identical subject whose completed cases
// are recorded in coverage.
func TrackMetadata(subject MetadataSubject, coverage *Coverage) MetadataSubject {
	subject.coverage = coverage
	return subject
}

// Identity returns the marker-derived Encoding component identity.
func (s MetadataSubject) Identity() plugin.Identity { return s.identity }

// MetadataFixture is one carrier block parsed by an Encoding case.
type MetadataFixture struct {
	carrier carrier.ID
	block   metadata.BlockID
	scope   metadata.Scope
	payload metadata.Blob
}

// MetadataInput constructs one immutable parse fixture.
func MetadataInput(slot carrier.ID, block metadata.BlockID, scope metadata.Scope, payload metadata.Blob) MetadataFixture {
	return MetadataFixture{carrier: slot, block: block, scope: scope, payload: payload}
}

// MetadataCase contains only component config, one carrier payload, and its
// expected semantic document and re-encoded payload.
type MetadataCase struct {
	Name   string
	Config config.Patch
	Input  MetadataFixture
	Want   MetadataExpectation
}

// MetadataExpectation describes either a semantic parse plus exact marshal
// result, or one structured resolver diagnostic.
type MetadataExpectation struct {
	document    metadata.Document
	payload     metadata.Blob
	failureCode string
	set         bool
}

// WantMetadata compares ordered entries, origins, unknown raw blocks, and the
// exact payload returned by Marshal.
func WantMetadata(document metadata.Document, payload metadata.Blob) MetadataExpectation {
	return MetadataExpectation{document: document, payload: payload, set: true}
}

// MetadataFails expects Parse or Marshal to report one diagnostic code.
func MetadataFails(code string) MetadataExpectation {
	return MetadataExpectation{failureCode: strings.TrimSpace(code), set: true}
}

// Metadata runs pure control-plane Encoding cases without inventing a Spec or
// Open operation for trait-only components. A testkit-owned anchor job keeps
// Plan, cancellation, and cleanup orchestration in the common runner.
func Metadata(t testing.TB, subject MetadataSubject, cases ...MetadataCase) {
	t.Helper()
	if err := validateMetadataSubject(subject); err != nil {
		t.Fatalf("testkit Metadata subject: %v", err)
	}
	if len(cases) == 0 {
		t.Fatalf("testkit Metadata requires at least one case")
	}
	for index := range cases {
		current := cases[index]
		name := current.Name
		if name == "" {
			name = fmt.Sprintf("case-%d", index+1)
		}
		runNamed(t, name, func(child testing.TB) {
			if !current.Input.valid() {
				child.Fatalf("testkit Metadata input fixture is invalid")
			}
			if !current.Want.valid() {
				child.Fatalf("testkit Metadata expectation is invalid")
			}
			executeCase(child, subject.identity, failureExpectation{}, func() (*scenarioCore, error) {
				return newMetadataScenario(subject, current)
			})
			subject.coverage.record(subject.identity)
		})
	}
}

func validateMetadataSubject(subject MetadataSubject) error {
	if subject.set.Empty() || subject.identity.IsZero() {
		return fmt.Errorf("composition or identity is empty")
	}
	component, ok := componentOf(subject.set, subject.identity)
	if !ok {
		return fmt.Errorf("component %s is absent from its Set", subject.identity)
	}
	encoding, ok := metadata.EncodingOf(component)
	if !ok || !encoding.Valid() {
		return fmt.Errorf("component %s has no valid Metadata Encoding trait", subject.identity)
	}
	return nil
}

func (f MetadataFixture) valid() bool {
	return f.carrier.Valid() && f.block != "" && f.scope.Valid() && f.payload.Valid()
}

func (e MetadataExpectation) valid() bool {
	if !e.set {
		return false
	}
	if e.failureCode != "" {
		return e.failureCode == strings.TrimSpace(e.failureCode)
	}
	return e.document.Scope().Valid() && e.payload.Valid()
}
