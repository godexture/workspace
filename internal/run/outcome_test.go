package run

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/internal/task"
	"github.com/godexture/godec/media/schema"
)

type consumerPanic struct{ Token string }
type releasePanic struct{ Token string }

type panickingSink struct{ templateOperator }

func (panickingSink) Write(context.Context, *flow.Item[int]) error {
	panic(consumerPanic{Token: outcomeSecret})
}

const outcomeSecret = "outcome-panic-secret"

// The value a task is holding is released by a deferred Drop that runs while
// the panic which stopped the task is already unwinding. A release that fails
// there used to become the panic, replacing the failure that actually stopped
// the work; both belong in the outcome, in the halves they belong to.
func TestAFailedReleaseDoesNotReplaceTheFailureThatStoppedTheTask(t *testing.T) {
	type inFlightID struct{}
	typ := schema.Define[inFlightID](schema.Traits[int]{
		Drop: func(int) { panic(releasePanic{Token: outcomeSecret}) },
	})
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	sinkLink, err := drive.NewSink("in", typ).OpenSink(panickingSink{templateOperator{shape: sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	reader := &templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1}}
	sourceTask, err := drive.NewSource("out", typ).OpenSource(reader, sinkLink)
	if err != nil {
		t.Fatal(err)
	}
	scope := journal.NewScope("sink")
	sourceTask.BindScope(scope)
	sinkLink.BindScope(scope)
	group := task.New(context.Background())
	if err := group.StartScoped("source", scope, sourceTask.Run); err != nil {
		t.Fatal(err)
	}

	report := group.Wait(context.Background())
	if len(report.Outcomes) != 1 {
		t.Fatalf("outcomes = %#v", report.Outcomes)
	}
	outcome := report.Outcomes[0]
	if outcome.Primary == nil || outcome.Primary.Kind != journal.TaskPanic {
		t.Fatalf("primary = %v, want the panic that stopped the task", outcome.Primary)
	}
	var panicErr *journal.PanicError
	if !errors.As(outcome.Primary.Err, &panicErr) || !strings.Contains(panicErr.Summary, "consumerPanic") {
		t.Fatalf("primary = %v, want the consumer's panic rather than the release that failed beside it", outcome.Primary)
	}
	if len(outcome.Cleanup) != 1 || outcome.Cleanup[0].Kind != journal.CleanupPanic {
		t.Fatalf("cleanup = %#v, want the release the task could not perform", outcome.Cleanup)
	}
	if len(outcome.Primary.Stack) == 0 {
		t.Error("the primary lost the stack it panicked from")
	}
	// Neither half renders a value the panicking code chose.
	for _, rendered := range []string{outcome.Primary.Error(), outcome.Cleanup[0].Error()} {
		if strings.Contains(rendered, outcomeSecret) {
			t.Error("the outcome exposes a recovered panic value")
		}
	}
}
