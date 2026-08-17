package host

import (
	"context"

	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/observe"
)

func (r *runner) emitLifecycle(node string, phase Phase, message string) {
	r.observe.Emit(observe.Event{Kind: observe.Lifecycle, Node: node, Phase: string(phase), Message: message})
}

// newObservationCollector wires delivery failure into the run's ledger at the
// moment it happens, not at the end.
//
// A sink that cannot be told what happened stops the run, so it is the reason
// the run stopped, and the run's own boundaries discover that as a cancellation
// afterwards. Recording it where it occurs is what makes it the earliest event
// rather than the last, and what makes everything the cancellation produces
// resolve back to it instead of reading as independent failures.
func (r *runner) newObservationCollector(options runOptions, ctx context.Context) *observe.Collector {
	if !options.observationSet {
		return nil
	}
	configuration := options.observation
	var sink observe.Sink
	if configuration.sink != nil {
		sink = func(ctx context.Context, event observe.Event) error {
			return configuration.sink.Emit(ctx, publicEvent(event))
		}
	}
	return observe.New(observe.Mode(configuration.mode), observe.Config{
		HistoryLimit:  configuration.history,
		DeliveryLimit: configuration.delivery,
		Sink:          sink,
		Context:       ctx,
		Fail: func(err error) {
			r.recordJournal(journal.WorkError, journal.Observation, "", "delivery", err, nil)
			r.stop(r.ledger.Stopped())
		},
	}, nil)
}

func (r *runner) finishObservation() {
	if r.observe == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.prepared.cleanupTimeout)
	// Collector.Close reports every terminal delivery failure through Fail
	// before returning, so its error is already in the run ledger. This call is
	// only the bounded join for the dispatcher.
	_ = r.observe.Close(ctx)
	cancel()
}

func (r *runner) finishSnapshots() {
	r.result.Diagnostics = r.diag.snapshot()
	observed := r.observe.Snapshot()
	r.result.Events = make([]Event, len(observed))
	for index, event := range observed {
		r.result.Events[index] = publicEvent(event)
	}
	summary := r.observe.Summary()
	r.result.Observation = ObservationSummary{HistoryDropped: summary.HistoryDropped, DeliveryDropped: summary.DeliveryDropped}
}

func publicEvent(event observe.Event) Event {
	return Event{
		Sequence: event.Sequence,
		Kind:     EventKind(event.Kind),
		Node:     event.Node,
		Edge:     event.Edge,
		Phase:    event.Phase,
		Code:     event.Code,
		Message:  event.Message,
		Items:    event.Items,
		Bytes:    event.Bytes,
		Media:    event.Media,
		HasMedia: event.HasMedia,
		At:       event.At,
		Detail:   copyDetail(event.Detail),
	}
}

func copyDetail(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
