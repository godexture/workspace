package host

import (
	"sync"

	"github.com/godexture/godec/diagnostic"
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
