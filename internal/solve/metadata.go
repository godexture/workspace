package solve

import (
	"fmt"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
)

type metadataPort struct {
	node string
	port string
}

// projectMetadataLosses connects a component's emitted bytes to the output
// boundary that commits them. A report cannot cross another component: after a
// component changes a byte stream, only that component can truthfully attach
// encoding evidence to its own output. Fan-out is safe because each boundary
// commits an independent copy of those same bytes.
func projectMetadataLosses(compiled graph.Graph, boundaries []plan.Boundary) ([]plan.PredictedMetadataLoss, error) {
	outputs := make(map[metadataPort][]plan.Boundary)
	for _, boundary := range boundaries {
		if boundary.Direction != plan.OutputBoundary {
			continue
		}
		key := metadataPort{node: boundary.Node, port: boundary.Port}
		outputs[key] = append(outputs[key], boundary)
	}
	forward := make(map[metadataPort][]metadataPort)
	for _, edge := range compiled.Edges() {
		from := metadataPort{node: edge.From().Node().String(), port: edge.From().ID()}
		forward[from] = append(forward[from], metadataPort{node: edge.To().Node().String(), port: edge.To().ID()})
	}

	var result []plan.PredictedMetadataLoss
	for _, node := range compiled.Nodes() {
		for _, report := range node.Compilation().MetadataReports() {
			from := metadataPort{node: node.ID().String(), port: report.Output}
			targets := forward[from]
			if len(targets) == 0 {
				return nil, metadataProjectionError(node.ID(), node.Component().String(), report.Output, "metadata report output does not reach an output boundary")
			}
			for _, target := range targets {
				boundaries := outputs[target]
				if len(boundaries) == 0 {
					return nil, metadataProjectionError(node.ID(), node.Component().String(), report.Output, "metadata report output crosses a component before an output boundary")
				}
				for _, boundary := range boundaries {
					result = append(result, plan.PredictedMetadataLoss{
						Output: boundary.Choice, Node: node.ID().String(), Component: node.Component().String(), Port: report.Output, Report: report.Report,
					})
				}
			}
		}
	}
	return result, nil
}

func metadataProjectionError(node job.NodeID, component, port, message string) error {
	return diagnostic.NewError(diagnostic.NewItem(
		"solve.metadata-report-boundary", diagnostic.ErrorSeverity,
		diagnostic.Path{Component: component, Descriptor: port}, message,
		map[string]string{"node": node.String(), "port": port},
	))
}

func strictMetadata(policy job.Policy, losses []plan.PredictedMetadataLoss) error {
	if policy.Metadata != job.StrictMetadata {
		return nil
	}
	var items []diagnostic.Item
	for _, value := range losses {
		if !value.Lossy() {
			continue
		}
		detail := map[string]string{
			"output": fmt.Sprint(value.Output), "node": value.Node, "port": value.Port,
			"carrier": value.Report.Carrier.String(), "encoding": value.Report.Encoding, "block": value.Report.Block,
			"key": value.Report.Loss.Key.String(), "kind": value.Report.Loss.Kind.String(), "native": value.Report.Loss.Native,
			"target": value.Report.Loss.Target.String(), "mapping": value.Report.Loss.Mapping.String(), "reason": value.Report.Loss.Detail,
		}
		items = append(items, diagnostic.NewItem("solve.metadata-loss", diagnostic.ErrorSeverity,
			diagnostic.Path{Component: value.Component, Descriptor: value.Port},
			"this conversion cannot carry metadata the job asked it to keep", detail))
	}
	if len(items) == 0 {
		return nil
	}
	return diagnostic.NewError(items...)
}

func metadataWarning(losses []plan.PredictedMetadataLoss) string {
	for _, value := range losses {
		if value.Lossy() {
			return "metadata loss is predicted"
		}
	}
	return ""
}
