package host

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/internal/bound"
	cancelpkg "github.com/godexture/godec/internal/cancel"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type conflictSinkID struct{}

func TestBoundaryEquivalencePanicAndErrorStaySafe(t *testing.T) {
	const secret = "equivalence-callback-secret"
	cases := []struct {
		name      string
		call      access.EquivalentFunc
		panicking bool
	}{
		{
			name:      "panic",
			panicking: true,
			call: func(context.Context, access.Reference, access.Reference) (bool, error) {
				panic(secret)
			},
		},
		{
			name: "error",
			call: func(_ context.Context, left, _ access.Reference) (bool, error) {
				return false, fmt.Errorf("cannot compare %s: %s", left.Canonical(), secret)
			},
		},
		{
			name: "malformed unwrap",
			call: func(context.Context, access.Reference, access.Reference) (bool, error) {
				return false, hostPanicUnwrap{}
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			capabilities := mustCapabilities(t, access.SequentialWrite)
			_, _, sink, _ := boundaryComponentsWith(nil, nil, []plugin.ComponentOption{
				access.Sink("memory", capabilities, access.AtomicReplace,
					func(context.Context, access.Reference, access.Selection) (access.Session, error) { return nil, nil },
					access.WithEquivalence(testCase.call)),
			})
			trait, ok := access.SinkOf(sink)
			if !ok {
				t.Fatal("sink trait is absent")
			}
			left, err := access.Parse("memory:left-secret")
			if err != nil {
				t.Fatal(err)
			}
			right, err := access.Parse("memory:right-secret")
			if err != nil {
				t.Fatal(err)
			}
			err = (&Host{}).validateBoundaries(context.Background(), []bound.Entry{
				conflictSinkEntry("a", left, trait),
				conflictSinkEntry("b", right, trait),
			})
			if err == nil {
				t.Fatal("boundary validation succeeded after equivalence callback failure")
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), left.Canonical()) {
				t.Fatalf("callback data leaked through boundary error: %v", err)
			}
			items := Diagnostics(err)
			if len(items) != 1 || items[0].Code != "prepare.boundary-identity" {
				t.Fatalf("boundary diagnostics = %#v", items)
			}
			if strings.Contains(items[0].Detail["cause"], secret) || items[0].Detail["cause"] == "" {
				t.Fatalf("unsafe callback cause detail = %#v", items[0].Detail)
			}
			failure := failureOf(PreparePhase, "", "", err)
			if testCase.panicking {
				if failure.Node != "b" || failure.Task != "access/equivalence" || len(failure.Stack) == 0 {
					t.Fatalf("panic provenance = %#v, want output node, callback task, and stack", failure)
				}
			} else if failure.Node != "" || failure.Task != "" || len(failure.Stack) != 0 {
				t.Fatalf("ordinary callback error acquired panic provenance: %#v", failure)
			}
		})
	}
}

func TestBoundaryEquivalenceJoinedCancellationStaysSafe(t *testing.T) {
	const secret = "joined-equivalence-secret"
	capabilities := mustCapabilities(t, access.SequentialWrite)
	_, _, sink, _ := boundaryComponentsWith(nil, nil, []plugin.ComponentOption{
		access.Sink("memory", capabilities, access.AtomicReplace,
			func(context.Context, access.Reference, access.Selection) (access.Session, error) { return nil, nil },
			access.WithEquivalence(func(ctx context.Context, _ access.Reference, _ access.Reference) (bool, error) {
				return false, errors.Join(context.Cause(ctx), errors.New(secret))
			})),
	})
	trait, ok := access.SinkOf(sink)
	if !ok {
		t.Fatal("sink trait is absent")
	}
	left, err := access.Parse("memory:left-secret")
	if err != nil {
		t.Fatal(err)
	}
	right, err := access.Parse("memory:right-secret")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("caller cancellation"))
	err = (&Host{}).validateBoundaries(ctx, []bound.Entry{
		conflictSinkEntry("a", left, trait),
		conflictSinkEntry("b", right, trait),
	})
	if err == nil {
		t.Fatal("boundary validation accepted a joined callback failure")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), left.Canonical()) {
		t.Fatalf("joined callback detail leaked: %v", err)
	}
	items := Diagnostics(err)
	if len(items) != 1 || items[0].Code != "prepare.boundary-identity" || items[0].Detail["cause"] == "" {
		t.Fatalf("joined callback diagnostic = %#v", items)
	}
}

func TestBoundaryEquivalencePureCancellationPassesThrough(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	stop := errors.New("caller cancellation")
	cancel(stop)
	if normalized := cancelpkg.Normalize(ctx, context.Cause(ctx)); normalized != context.Cause(ctx) {
		t.Fatal("the exact caller cancellation was not recognized")
	}
}

func TestBoundaryEquivalenceWrappedCancellationReturnsTrustedCause(t *testing.T) {
	const secret = "wrapped-equivalence-secret"
	stopCause := errors.New("caller cancellation")
	capabilities := mustCapabilities(t, access.SequentialWrite)
	_, _, sink, _ := boundaryComponentsWith(nil, nil, []plugin.ComponentOption{
		access.Sink("memory", capabilities, access.AtomicReplace,
			func(context.Context, access.Reference, access.Selection) (access.Session, error) { return nil, nil },
			access.WithEquivalence(func(_ context.Context, left, _ access.Reference) (bool, error) {
				return false, fmt.Errorf("provider %s target %s: %w", secret, left.Canonical(), stopCause)
			})),
	})
	trait, ok := access.SinkOf(sink)
	if !ok {
		t.Fatal("sink trait is absent")
	}
	left, err := access.Parse("memory:left-secret")
	if err != nil {
		t.Fatal(err)
	}
	right, err := access.Parse("memory:right-secret")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(stopCause)
	err = (&Host{}).validateBoundaries(ctx, []bound.Entry{
		conflictSinkEntry("a", left, trait),
		conflictSinkEntry("b", right, trait),
	})
	if err == nil || !errors.Is(err, stopCause) {
		t.Fatalf("wrapped cancellation error = %v, want trusted cause %v", err, stopCause)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), left.Canonical()) {
		t.Fatalf("wrapped callback detail leaked: %v", err)
	}
	for _, item := range Diagnostics(err) {
		if strings.Contains(item.Message, secret) || strings.Contains(item.Message, left.Canonical()) || strings.Contains(item.Path.String(), left.Canonical()) {
			t.Fatalf("wrapped callback detail leaked through diagnostics: %#v", item)
		}
	}
}

func TestBoundaryEquivalenceLiveWrappedCancellationStaysSafe(t *testing.T) {
	const secret = "live-cancellation-secret"
	capabilities := mustCapabilities(t, access.SequentialWrite)
	_, _, sink, _ := boundaryComponentsWith(nil, nil, []plugin.ComponentOption{
		access.Sink("memory", capabilities, access.AtomicReplace,
			func(context.Context, access.Reference, access.Selection) (access.Session, error) { return nil, nil },
			access.WithEquivalence(func(context.Context, access.Reference, access.Reference) (bool, error) {
				return false, fmt.Errorf("provider detail %s: %w", secret, context.Canceled)
			})),
	})
	trait, ok := access.SinkOf(sink)
	if !ok {
		t.Fatal("sink trait is absent")
	}
	left, err := access.Parse("memory:left-secret")
	if err != nil {
		t.Fatal(err)
	}
	right, err := access.Parse("memory:right-secret")
	if err != nil {
		t.Fatal(err)
	}
	err = (&Host{}).validateBoundaries(context.Background(), []bound.Entry{
		conflictSinkEntry("a", left, trait),
		conflictSinkEntry("b", right, trait),
	})
	if err == nil {
		t.Fatal("boundary validation accepted a live-context provider failure")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), left.Canonical()) {
		t.Fatalf("live callback detail leaked: %v", err)
	}
	items := Diagnostics(err)
	if len(items) != 1 || items[0].Code != "prepare.boundary-identity" || items[0].Detail["cause"] == "" {
		t.Fatalf("live callback diagnostic = %#v", items)
	}
}

func conflictSinkEntry(node string, reference access.Reference, trait access.SinkTrait) bound.Entry {
	return bound.Sink(plan.Boundary{
		Direction:            plan.OutputBoundary,
		Kind:                 plan.ProviderBoundary,
		Choice:               0,
		Node:                 node,
		Port:                 "in",
		Component:            plugin.IdentityOf[conflictSinkID]().String(),
		Scheme:               reference.Scheme(),
		Reference:            reference.Display(),
		ReferenceFingerprint: reference.Fingerprint().String(),
		Available:            []access.Capability{access.SequentialWrite},
		Effective:            []access.Capability{access.SequentialWrite},
		Selected:             []access.Capability{access.SequentialWrite},
	}, reference, trait)
}
