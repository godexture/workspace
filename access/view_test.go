package access

import (
	"context"
	"errors"
	"testing"
)

type capabilityViewSession struct {
	capabilities Capabilities
}

func (s capabilityViewSession) Capabilities() Capabilities { return cloneCapabilities(s.capabilities) }
func (capabilityViewSession) Close() error                 { return nil }
func (capabilityViewSession) Read(context.Context, []byte) (int, error) {
	return 0, nil
}
func (capabilityViewSession) ReadAt(context.Context, []byte, int64) (int, error) {
	return 0, nil
}
func (capabilityViewSession) Write(context.Context, []byte) (int, error) {
	return 0, nil
}
func (capabilityViewSession) WriteAt(context.Context, []byte, int64) (int, error) {
	return 0, nil
}

type capabilityOnlySession struct {
	capabilities Capabilities
}

func (s capabilityOnlySession) Capabilities() Capabilities { return cloneCapabilities(s.capabilities) }
func (capabilityOnlySession) Close() error                 { return nil }

func TestViewsForNarrowsSelectedOperations(t *testing.T) {
	available, err := NewCapabilities(SequentialRead, RandomRead, StableSize, SequentialWrite, RandomWrite)
	if err != nil {
		t.Fatal(err)
	}
	selection, ok := Select(available, NewRequirements(AnyOf(SequentialRead, RandomWrite, StableSize)))
	if !ok {
		t.Fatal("capability selection failed")
	}
	views, err := viewsFor(capabilityViewSession{capabilities: available}, selection)
	if err != nil {
		t.Fatal(err)
	}
	if views.sequential == nil || views.patcher == nil || views.random != nil || views.appender != nil {
		t.Fatalf("narrow views = %#v", views)
	}
}

func TestViewsForRejectsMissingSelectedOperation(t *testing.T) {
	available, err := NewCapabilities(SequentialWrite)
	if err != nil {
		t.Fatal(err)
	}
	selection, ok := Select(available, NewRequirements(AnyOf(SequentialWrite)))
	if !ok {
		t.Fatal("capability selection failed")
	}
	if _, err := viewsFor(capabilityOnlySession{capabilities: available}, selection); !errors.Is(err, ErrCapabilityView) {
		t.Fatalf("missing Appender error = %v", err)
	}
}
