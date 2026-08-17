// Scenario construction: one subject plus a testkit-owned source and sink,
// composed into the Host, Job, and graph a case never has to write.
package testkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

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
	active      *activeRun
	selected    plugin.Identity
	purity      func(context.Context) (string, error)
	inspectPlan func(plan.Plan) error
	cancelCheck func() error
	finish      func() error
	cleanup     func() error
}

// activeRun is enabled only by the cancellation scenario.  Fixture callbacks
// then stop at their first execution and wait on the run context, giving the
// runner evidence that execution is live before it requests cancellation.
// Planning and the ordinary success path leave it disabled, so plugin authors
// do not need a scheduler or a test-only hook in their Case.
type activeRun struct {
	enabled atomic.Bool
	target  sync.Once
	reached chan struct{}
	release sync.Once
	open    chan struct{}
}

func newActiveRun() *activeRun {
	return &activeRun{reached: make(chan struct{}), open: make(chan struct{})}
}

func (a *activeRun) enable() {
	if a != nil {
		a.enabled.Store(true)
	}
}

func (a *activeRun) mark() {
	if a == nil || !a.enabled.Load() {
		return
	}
}

func (a *activeRun) block(ctx context.Context) error {
	if a == nil || !a.enabled.Load() {
		return nil
	}
	a.mark()
	a.target.Do(func() { close(a.reached) })
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-a.open:
		if err := ctx.Err(); err != nil {
			return context.Cause(ctx)
		}
		return nil
	}
}

func (a *activeRun) wait(timeout time.Duration) error {
	if a == nil {
		return errors.New("active cancellation gate is absent")
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-a.reached:
		return nil
	case <-timer.C:
		return errors.New("active cancellation callback was not reached")
	}
}

func (a *activeRun) unblock() {
	if a != nil {
		a.release.Do(func() { close(a.open) })
	}
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

type scenarioSettings struct {
	reject *rejection
	active *activeRun
}

func withRejection(value *rejection) scenarioOption {
	return func(settings *scenarioSettings) { settings.reject = value }
}

func newScenario[I, O any](kind runnerKind, subject Subject[I, O], patch config.Patch, input Fixture[I], output recorder[O], options ...scenarioOption) (*scenarioCore, error) {
	settings := scenarioSettings{active: newActiveRun()}
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
		active: settings.active,
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
