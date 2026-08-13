// Scenario construction: one subject plus a testkit-owned source and sink,
// composed into the Host, Job, and graph a case never has to write.
package testkit

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type scenarioCore struct {
	host        *host.Host
	job         job.Job
	state       *lifecycleState
	selected    plugin.Identity
	purity      func(context.Context) (string, error)
	inspectPlan func(plan.Plan) error
	cancelCheck func() error
	finish      func() error
	cleanup     func() error
}

func (s *scenarioCore) close() error {
	if s == nil {
		return nil
	}
	if s.cleanup != nil {
		return s.cleanup()
	}
	return nil
}

// scenarioOption adjusts how the testkit fixture pair behaves. Cases never see
// these; they exist so the runner can drive the same subject through its
// success and failure paths.
type scenarioOption func(*scenarioSettings)

type scenarioSettings struct{ reject *rejection }

func withRejection(value *rejection) scenarioOption {
	return func(settings *scenarioSettings) { settings.reject = value }
}

func newScenario[I, O any](kind runnerKind, subject Subject[I, O], patch config.Patch, input Fixture[I], output recorder[O], options ...scenarioOption) (*scenarioCore, error) {
	settings := scenarioSettings{}
	for _, option := range options {
		option(&settings)
	}
	state := &lifecycleState{}
	fixture := fixtureDefinition(kind, subject, &input, output, state, settings)
	set := subject.set.Add(fixture)
	instance, err := host.New(host.Plugins(set))
	if err != nil {
		return nil, err
	}
	request, closers, err := scenarioJob(kind, subject, set, patch, &input, settings)
	if err != nil {
		return nil, err
	}
	return &scenarioCore{
		host:   instance,
		job:    request,
		state:  state,
		finish: output.finish,
		cleanup: func() error {
			var problems []error
			problems = append(problems, input.close())
			for index := len(closers) - 1; index >= 0; index-- {
				problems = append(problems, closers[index].Close())
			}
			return errors.Join(problems...)
		},
	}, nil
}

func scenarioJob[I, O any](kind runnerKind, subject Subject[I, O], set plugin.Set, patch config.Patch, input *Fixture[I], settings scenarioSettings) (job.Job, []io.Closer, error) {
	source := job.NewNode("fixture-source", plugin.IdentityOf[fixtureSourceID](), config.NewPatch())
	target := job.NewNode("subject", subject.identity, patch)
	sink := job.NewNode("fixture-sink", plugin.IdentityOf[fixtureSinkID](), config.NewPatch())
	component, ok := componentOf(set, subject.identity)
	if !ok {
		return job.Job{}, nil, fmt.Errorf("subject %s is absent from its Set", subject.identity)
	}

	if kind == formatRunner {
		read, hasRead := mediaformat.ReadOf(component)
		write, hasWrite := mediaformat.WriteOf(component)
		if hasRead == hasWrite {
			return job.Job{}, nil, errors.New("Format subject must carry exactly one read or write trait")
		}
		if hasRead {
			return readFormatJob(subject, input, read, target, sink, settings)
		}
		return writeFormatJob(subject, input, write, source, target)
	}

	nodes := []job.Node{source, target, sink}
	edges := []job.Edge{job.Connect(job.At(source.ID(), "out"), job.At(target.ID(), subject.input.id))}
	nodes, edges = appendRejection(nodes, edges, subject.output.id, target, sink, "in", settings)
	graph, err := job.NewGraph(nodes, edges)
	if err != nil {
		return job.Job{}, nil, err
	}
	request, err := job.New(nil, nil, graph)
	return request, nil, err
}

// appendRejection routes the subject through the rejecting processor before
// its downstream consumer, so the failure reaches the subject's own Emit.
func appendRejection(nodes []job.Node, edges []job.Edge, outputPort string, target, consumer job.Node, consumerPort string, settings scenarioSettings) ([]job.Node, []job.Edge) {
	if settings.reject == nil {
		return nodes, append(edges, job.Connect(job.At(target.ID(), outputPort), job.At(consumer.ID(), consumerPort)))
	}
	reject := job.NewNode("fixture-reject", plugin.IdentityOf[fixtureRejectID](), config.NewPatch())
	return append(nodes, reject), append(edges,
		job.Connect(job.At(target.ID(), outputPort), job.At(reject.ID(), "in")),
		job.Connect(job.At(reject.ID(), "out"), job.At(consumer.ID(), consumerPort)),
	)
}

func readFormatJob[I, O any](subject Subject[I, O], input *Fixture[I], trait mediaformat.ReadTrait, target, sink job.Node, settings scenarioSettings) (job.Job, []io.Closer, error) {
	if subject.input.schema.Identity() != access.Bytes().Identity() {
		return job.Job{}, nil, errors.New("read Format input schema must be access.Bytes")
	}
	_ = trait
	_ = input
	reference, err := access.NewReference("testkit", "input")
	if err != nil {
		return job.Job{}, nil, err
	}
	boundary, err := job.InputFromReference(reference)
	if err != nil {
		return job.Job{}, nil, err
	}
	nodes, edges := appendRejection([]job.Node{target, sink}, nil, subject.output.id, target, sink, "in", settings)
	graph, err := job.NewGraph(nodes, edges)
	if err != nil {
		return job.Job{}, nil, err
	}
	request, err := job.New([]job.Input{boundary}, nil, graph)
	return request, nil, err
}

func writeFormatJob[I, O any](subject Subject[I, O], input *Fixture[I], trait mediaformat.WriteTrait, source, target job.Node) (job.Job, []io.Closer, error) {
	if subject.output.schema.Identity() != access.Writes().Identity() {
		return job.Job{}, nil, errors.New("write Format output schema must be access.Writes")
	}
	_ = trait
	_ = input
	reference, err := access.NewReference("testkit", "output")
	if err != nil {
		return job.Job{}, nil, err
	}
	boundary, err := job.OutputToReference(reference)
	if err != nil {
		return job.Job{}, nil, err
	}
	graph, err := job.NewGraph(
		[]job.Node{source, target},
		[]job.Edge{job.Connect(job.At(source.ID(), "out"), job.At(target.ID(), subject.input.id))},
	)
	if err != nil {
		return job.Job{}, nil, err
	}
	request, err := job.New(nil, []job.Output{boundary}, graph)
	return request, nil, err
}
