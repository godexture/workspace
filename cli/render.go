package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/plan"
)

type eventRenderer struct{ destination io.Writer }

func (r *eventRenderer) Emit(_ context.Context, event host.Event) error {
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

func renderPlan(destination io.Writer, selected plan.Plan) error {
	if _, err := fmt.Fprintf(destination, "plan %s\n", selected.Fingerprint()); err != nil {
		return err
	}
	for _, node := range selected.Nodes() {
		origin := "requested"
		if node.Origin == plan.Automatic {
			origin = "automatic"
		}
		if _, err := fmt.Fprintf(destination, "node %s component=%s origin=%s", node.ID, node.Component, origin); err != nil {
			return err
		}
		if node.Reason != "" {
			if _, err := fmt.Fprintf(destination, " reason=%s", node.Reason); err != nil {
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
			if _, err := fmt.Fprintf(destination, "  config %s=%s source=%s\n", field.ID, value, field.Source); err != nil {
				return err
			}
		}
	}
	for _, boundary := range selected.Boundaries() {
		direction := "input"
		if boundary.Direction == plan.OutputBoundary {
			direction = "output"
		}
		if _, err := fmt.Fprintf(destination, "boundary %s[%d] node=%s component=%s", direction, boundary.Choice, boundary.Node, boundary.Component); err != nil {
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
	return nil
}

func capabilities(values []access.Capability) string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return strings.Join(result, ",")
}

func renderResult(stdout, stderr io.Writer, result host.Result, runErr error) error {
	var failures []error
	if result.Observation.HistoryDropped != 0 || result.Observation.DeliveryDropped != 0 {
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
		keys := make([]string, 0, len(item.Detail))
		for key := range item.Detail {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			_, err := fmt.Fprintf(destination, " %s=%s", key, item.Detail[key])
			failures = append(failures, err)
		}
		_, err = io.WriteString(destination, "\n")
		failures = append(failures, err)
	}
	return errorsJoin(failures)
}

func errorsJoin(values []error) error {
	return errors.Join(values...)
}
