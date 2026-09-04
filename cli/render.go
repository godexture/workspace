package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/internal/evidence"
	"github.com/godexture/godec/plan"
)

type eventRenderer struct {
	destination io.Writer
	mu          sync.Mutex
	stopped     atomic.Bool
	seen        map[uint64]struct{}
	printed     map[uint64]struct{}
	deferred    map[uint64]host.Event
	next        uint64
	waiting     bool
}

func (r *eventRenderer) Emit(_ context.Context, event host.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped.Load() {
		return nil
	}
	if r.seen == nil {
		r.seen = make(map[uint64]struct{})
		r.printed = make(map[uint64]struct{})
		r.deferred = make(map[uint64]host.Event)
	}
	r.seen[event.Sequence] = struct{}{}
	if r.waiting || event.Sequence != r.next {
		r.waiting = true
		r.deferred[event.Sequence] = event
		return nil
	}
	if err := r.render(event); err != nil {
		return err
	}
	r.printed[event.Sequence] = struct{}{}
	r.next++
	return nil
}

func (r *eventRenderer) finish(events []host.Event, summary host.ObservationSummary) (bool, error) {
	r.stopped.Store(true)
	if !r.mu.TryLock() {
		return false, errEventRendererActive
	}
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = make(map[uint64]struct{})
		r.printed = make(map[uint64]struct{})
		r.deferred = make(map[uint64]host.Event)
	}
	available := make(map[uint64]host.Event, len(r.deferred)+len(events))
	for sequence, event := range r.deferred {
		available[sequence] = event
	}
	for _, event := range events {
		r.seen[event.Sequence] = struct{}{}
		available[event.Sequence] = event
	}
	sequences := make([]uint64, 0, len(available))
	for sequence := range available {
		if _, ok := r.printed[sequence]; !ok {
			sequences = append(sequences, sequence)
		}
	}
	sort.Slice(sequences, func(left, right int) bool { return sequences[left] < sequences[right] })
	for _, sequence := range sequences {
		if err := r.render(available[sequence]); err != nil {
			return observationLost(r.seen, events, summary), err
		}
		r.printed[sequence] = struct{}{}
	}
	return observationLost(r.seen, events, summary), nil
}

var errEventRendererActive = errors.New("event renderer delivery exceeded cleanup timeout")

func observationLost(seen map[uint64]struct{}, events []host.Event, summary host.ObservationSummary) bool {
	total := summary.HistoryDropped + uint64(len(events))
	return uint64(len(seen)) < total
}

func (r *eventRenderer) render(event host.Event) error {
	switch event.Kind {
	case host.ProgressEvent:
		_, err := fmt.Fprintf(r.destination, "progress sequence=%d edge=%s items=%d bytes=%d\n", event.Sequence, event.Edge, event.Items, event.Bytes)
		return err
	case host.DiagnosticEvent:
		_, err := fmt.Fprintf(r.destination, "diagnostic sequence=%d code=%s message=%s\n", event.Sequence, event.Code, event.Message)
		return err
	case host.LifecycleEvent:
		_, err := fmt.Fprintf(r.destination, "lifecycle sequence=%d node=%s phase=%s state=%s\n", event.Sequence, event.Node, event.Phase, event.Message)
		return err
	}
	return nil
}

// reasons translate the planner's machine-readable codes. The Plan keeps the
// code; only this projection is for people, and an unmapped code prints as
// itself rather than disappearing.
var reasons = map[string]string{
	"format.probe":          "detected from the file content",
	"format.fallback":       "chosen from the explicit format, without content evidence",
	"format.output":         "chosen for the requested output format",
	"graph.schema-mismatch": "inserted to connect two incompatible ports",
}

func explain(reason string) string {
	if text, ok := reasons[reason]; ok {
		return text
	}
	return reason
}

func renderPlan(destination io.Writer, selected plan.Plan) error {
	if _, err := fmt.Fprintf(destination, "plan %s\n", selected.Fingerprint()); err != nil {
		return err
	}
	for _, node := range selected.Nodes() {
		origin := "requested"
		if node.Origin == plan.Automatic {
			origin = "automatic"
		}
		if _, err := fmt.Fprintf(destination, "node %s %s (%s)", node.ID, displayName(node.DisplayName, node.Component), origin); err != nil {
			return err
		}
		if node.Reason != "" {
			if _, err := fmt.Fprintf(destination, ": %s", explain(node.Reason)); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(destination, "\n"); err != nil {
			return err
		}
		for _, field := range node.Config.Fields() {
			value := field.Value
			if field.Redacted {
				value = "<redacted>"
			}
			if _, err := fmt.Fprintf(destination, "  config %s=%s source=%s\n", field.ID, value, field.Source.String()); err != nil {
				return err
			}
		}
	}
	for _, boundary := range selected.Boundaries() {
		direction := "input"
		if boundary.Direction == plan.OutputBoundary {
			direction = "output"
		}
		if _, err := fmt.Fprintf(destination, "boundary %s[%d] %s %s", direction, boundary.Choice, boundary.Node, boundary.Reference); err != nil {
			return err
		}
		if boundary.Scheme != "" {
			if _, err := fmt.Fprintf(destination, " scheme=%s capabilities=%s", boundary.Scheme, capabilities(boundary.Selected)); err != nil {
				return err
			}
		}
		if !boundary.Spool.IsZero() {
			if _, err := fmt.Fprintf(destination, " spool=%d/%d", boundary.Spool.PredictedBytes(), boundary.Spool.MaximumBytes()); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(destination, "\n"); err != nil {
			return err
		}
	}
	for _, warning := range selected.Warnings() {
		if _, err := fmt.Fprintf(destination, "warning %s\n", warning); err != nil {
			return err
		}
	}
	for _, value := range selected.PredictedMetadataLosses() {
		if err := renderPredictedMetadataLoss(destination, value); err != nil {
			return err
		}
	}
	return nil
}

func renderPredictedMetadataLoss(destination io.Writer, value plan.PredictedMetadataLoss) error {
	detail := evidence.MetadataLoss(value.Report)
	if _, err := fmt.Fprintf(destination, "metadata-loss output=%d node=%s component=%s port=%s", value.Output, value.Node, value.Component, value.Port); err != nil {
		return err
	}
	for _, key := range sortedDetailKeys(detail) {
		if _, err := fmt.Fprintf(destination, " %s=%s", key, detail[key]); err != nil {
			return err
		}
	}
	_, err := io.WriteString(destination, "\n")
	return err
}

func capabilities(values []access.Capability) string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return strings.Join(result, ",")
}

func renderResult(stdout, stderr io.Writer, result host.Result, runErr error, observationLost bool) error {
	var failures []error
	if observationLost {
		_, err := fmt.Fprintf(stderr, "warning observation-loss history=%d delivery=%d\n", result.Observation.HistoryDropped, result.Observation.DeliveryDropped)
		failures = append(failures, err)
	}
	if len(result.Diagnostics) != 0 {
		failures = append(failures, renderItems(stderr, result.Diagnostics))
	} else if runErr != nil {
		failures = append(failures, renderError(stderr, runErr))
	}
	for _, output := range result.Outputs {
		_, err := fmt.Fprintf(stdout, "output node=%s state=%s\n", output.Node, output.State)
		failures = append(failures, err)
	}
	return errorsJoin(failures)
}

func renderUsageError(destination io.Writer, err error) error {
	_, writeErr := fmt.Fprintf(destination, "usage error: %v\n%s", err, usage)
	return writeErr
}

func renderError(destination io.Writer, err error) error {
	items := diagnostic.ItemsOf(err)
	if len(items) == 0 {
		_, writeErr := fmt.Fprintf(destination, "error: %v\n", err)
		return writeErr
	}
	return renderItems(destination, items)
}

func renderItems(destination io.Writer, items []diagnostic.Item) error {
	var failures []error
	for _, item := range items {
		location := item.Path.String()
		if location != "" {
			location += ": "
		}
		_, err := fmt.Fprintf(destination, "%s%s: %s: %s", location, item.Severity, item.Code, item.Message)
		failures = append(failures, err)
		for _, key := range sortedDetailKeys(item.Detail) {
			_, err := fmt.Fprintf(destination, " %s=%s", key, item.Detail[key])
			failures = append(failures, err)
		}
		_, err = io.WriteString(destination, "\n")
		failures = append(failures, err)
	}
	return errorsJoin(failures)
}

func sortedDetailKeys(detail map[string]string) []string {
	keys := make([]string, 0, len(detail))
	for key := range detail {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func errorsJoin(values []error) error {
	return errors.Join(values...)
}

// displayName prefers the name the component publishes. The marker identity is
// a Go type path, which identifies a component precisely and tells a person
// nothing, so it stays in diagnostics rather than the Plan preview.
func displayName(name, identity string) string {
	if name != "" {
		return name
	}
	return identity
}
