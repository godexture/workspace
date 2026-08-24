package host

import (
	"strconv"
	"sync"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/journal"
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
	if ownership, ok := failure.Err.(*journal.OwnershipError); ok {
		detail["live"] = strconv.FormatInt(ownership.Live, 10)
		detail["overrelease"] = strconv.FormatUint(ownership.Overrelease, 10)
	}
	d.append(diagnostic.NewItem(code, diagnostic.ErrorSeverity, diagnostic.Path{Component: failure.Node}, failure.Err.Error(), detail))
}

func (d *diagnosticLog) metadataLoss(value ActualMetadataLoss) {
	detail := map[string]string{
		"output": strconv.Itoa(value.Output), "node": value.Node, "port": value.Port,
		"carrier": value.Report.Carrier.String(), "encoding": value.Report.Encoding, "block": value.Report.Block,
		"key": value.Report.Loss.Key.String(), "kind": value.Report.Loss.Kind.String(), "native": value.Report.Loss.Native,
		"target": value.Report.Loss.Target.String(), "mapping": value.Report.Loss.Mapping.String(), "reason": value.Report.Loss.Detail,
	}
	d.append(diagnostic.NewItem("host.metadata-loss", diagnostic.WarningSeverity,
		diagnostic.Path{Component: value.Component, Descriptor: value.Port},
		"committed output could not carry metadata exactly", detail))
}

// suppressed records repetition the run counted rather than copied, so a
// surface that only reads diagnostics still learns the total and the fact that
// detail was dropped.
func (d *diagnosticLog) suppressed(value Suppressed) {
	detail := map[string]string{
		"occurrences": strconv.FormatUint(value.Occurrences, 10),
		"retained":    strconv.FormatUint(value.Retained, 10),
		"omitted":     strconv.FormatUint(value.Omitted(), 10),
		"class":       value.Class,
		"kind":        value.Kind,
		"truncated":   strconv.FormatBool(value.Truncated),
	}
	if value.Task != "" {
		detail["task"] = value.Task
	}
	if value.Classes > 1 || value.ClassesTruncated {
		detail["classes"] = strconv.FormatUint(value.Classes, 10)
	}
	if value.ClassesTruncated {
		detail["classesTruncated"] = "true"
	}
	d.append(diagnostic.NewItem("host.suppressed."+string(value.Phase), diagnostic.ErrorSeverity, diagnostic.Path{Component: value.Node}, value.Error(), detail))
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
