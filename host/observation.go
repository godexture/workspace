package host

import (
	"sync"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/observe"
)

type diagnosticLog struct {
	mu    sync.Mutex
	items []diagnostic.Item
}

func (d *diagnosticLog) sink(node string) diagnostic.Sink {
	return diagnostic.SinkFunc(func(item diagnostic.Item) {
		detail := copyDetail(item.Detail)
		if item.Path.Component != "" && item.Path.Component != node {
			if detail == nil {
				detail = make(map[string]string)
			}
			detail["reportedComponent"] = item.Path.Component
		}
		path := diagnostic.Path{
			Component:  node,
			Descriptor: item.Path.Descriptor,
			Fields:     append([]string(nil), item.Path.Fields...),
		}
		d.append(diagnostic.NewItem(item.Code, item.Severity, path, item.Message, detail))
	})
}

func (d *diagnosticLog) failure(code string, failure Failure) {
	detail := make(map[string]string)
	if failure.Task != "" {
		detail["task"] = failure.Task
	}
	if len(failure.Stack) != 0 {
		detail["panic"] = "true"
	}
	d.append(diagnostic.NewItem(code, diagnostic.ErrorSeverity, diagnostic.Path{Component: failure.Node}, failure.Err.Error(), detail))
}

func (d *diagnosticLog) append(item diagnostic.Item) {
	d.mu.Lock()
	d.items = append(d.items, item)
	d.mu.Unlock()
}

func (d *diagnosticLog) snapshot() []diagnostic.Item {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]diagnostic.Item, len(d.items))
	for index, item := range d.items {
		result[index] = diagnostic.NewItem(item.Code, item.Severity, item.Path, item.Message, item.Detail)
	}
	return result
}

func (r *runner) emitLifecycle(node string, phase Phase, message string) {
	r.observe.Emit(observe.Event{Kind: observe.Lifecycle, Node: node, Phase: string(phase), Message: message})
}

func (r *runner) finishSnapshots() {
	r.result.Diagnostics = r.diag.snapshot()
	observed := r.observe.Snapshot()
	r.result.Events = make([]Event, len(observed))
	for index, event := range observed {
		r.result.Events[index] = Event{
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
