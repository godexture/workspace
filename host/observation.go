package host

import (
	"context"
	"errors"

	"github.com/godexture/godec/internal/observe"
)

func (r *runner) emitLifecycle(node string, phase Phase, message string) {
	r.observe.Emit(observe.Event{Kind: observe.Lifecycle, Node: node, Phase: string(phase), Message: message})
}

func newObservationCollector(options runOptions, ctx context.Context, cancel context.CancelCauseFunc) *observe.Collector {
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
		Fail:          cancel,
	}, nil)
}

func (r *runner) finishObservation() {
	if r.observe == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.prepared.cleanupTimeout)
	err := r.observe.Close(ctx)
	cancel()
	if err == nil {
		return
	}
	failure := failureOf(ObservationPhase, "", "delivery", err)
	if r.result.Primary == nil {
		r.setPrimary(failure)
		return
	}
	r.addCleanup(failure)
}

func (r *runner) observationFailure(failure Failure) Failure {
	if r.observe == nil {
		return failure
	}
	err := r.observe.Err()
	if err == nil || !errors.Is(context.Cause(r.ctx), err) && !errors.Is(failure.Err, err) {
		return failure
	}
	return failureOf(ObservationPhase, "", "delivery", err)
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
