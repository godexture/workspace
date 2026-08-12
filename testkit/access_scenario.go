package testkit

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/buffer"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type accessFixturePluginID struct{}
type accessFixtureConfigID struct{}
type accessFixtureFormatID struct{}
type accessReadPassID struct{}
type accessReadSinkID struct{}
type accessWriteSourceID struct{}
type accessWritePassID struct{}

type accessFixtureConfig struct{}
type accessFixturePlan struct{ shape flow.Shape }

var accessOriginalBytes = []byte("testkit-original")

func newAccessScenario(subject AccessSubject, direction access.Direction, input AccessFixture, want AccessExpectation) (*scenarioCore, error) {
	target, err := input.open(context.Background())
	if err != nil {
		return nil, err
	}
	if !target.valid() {
		return nil, closeAccessTarget(target, errors.New("Access fixture returned an invalid target"))
	}
	payload := input.cloneBytes()
	initial := payload
	if direction == access.SinkDirection {
		initial = append([]byte(nil), accessOriginalBytes...)
	}
	if err := target.seed(context.Background(), initial); err != nil {
		return nil, closeAccessTarget(target, err)
	}

	state := &lifecycleState{}
	var fixture plugin.Definition
	var request job.Job
	var ownedInput Fixture[buffer.Handle]
	var output *byteStreamRecorder
	if direction == access.SourceDirection {
		output = &byteStreamRecorder{want: append([]byte(nil), want.bytes...)}
		fixture = accessReadFixture(want.requirements, output, state)
		request, err = accessReadJob(target.reference)
	} else {
		ownedInput = ByteInput(payload)
		if !ownedInput.valid() {
			err = errors.New("Access sink fixture could not allocate its byte input")
		} else {
			fixture = accessWriteFixture(want.requirements, &ownedInput, state)
			request, err = accessWriteJob(target.reference)
		}
	}
	if err != nil {
		return nil, closeAccessTarget(target, ownedInput.close(), err)
	}
	set := subject.set.Add(fixture)
	instance, err := host.New(host.Plugins(set))
	if err != nil {
		return nil, closeAccessTarget(target, ownedInput.close(), err)
	}

	checkTarget := func(expected []byte) error {
		actual, readErr := target.read(context.Background())
		residue, residueErr := target.residue(context.Background())
		var mismatch error
		if readErr == nil && !bytes.Equal(actual, expected) {
			mismatch = fmt.Errorf("target bytes = %x, want %x", actual, expected)
		}
		if residueErr == nil && len(residue) != 0 {
			mismatch = errors.Join(mismatch, fmt.Errorf("transaction residue = %v", residue))
		}
		return errors.Join(readErr, residueErr, mismatch)
	}
	finish := func() error {
		var outputErr error
		if output != nil {
			outputErr = output.finish()
		}
		expectedTarget := payload
		if direction == access.SinkDirection {
			expectedTarget = want.bytes
		}
		return errors.Join(outputErr, checkTarget(expectedTarget))
	}
	cancelCheck := func() error { return checkTarget(initial) }
	cleanup := func() error {
		residue, residueErr := target.residue(context.Background())
		if residueErr == nil && len(residue) != 0 {
			residueErr = fmt.Errorf("transaction residue at cleanup = %v", residue)
		}
		return closeAccessTarget(target, ownedInput.close(), residueErr)
	}
	return &scenarioCore{
		host:  instance,
		job:   request,
		state: state,
		inspectPlan: func(selected plan.Plan) error {
			return inspectAccessPlan(selected, subject, target.reference, direction, want.requirements)
		},
		cancelCheck: cancelCheck,
		finish:      finish,
		cleanup:     cleanup,
	}, nil
}

func inspectAccessPlan(selected plan.Plan, subject AccessSubject, reference access.Reference, direction access.Direction, requirements access.Requirements) error {
	component, ok := componentOf(subject.set, subject.identity)
	if !ok {
		return fmt.Errorf("Access subject %s is absent", subject.identity)
	}
	var available access.Capabilities
	var scheme string
	if direction == access.SourceDirection {
		trait, present := access.SourceOf(component)
		if !present {
			return errors.New("Access source trait disappeared")
		}
		available = trait.Capabilities()
		scheme = trait.Scheme()
	} else {
		trait, present := access.SinkOf(component)
		if !present {
			return errors.New("Access sink trait disappeared")
		}
		available = trait.Capabilities()
		scheme = trait.Scheme()
	}
	selection, ok := access.Select(available, requirements)
	if !ok {
		return fmt.Errorf("declared capabilities %v do not satisfy fixture requirements", available.Values())
	}
	var found []plan.Boundary
	for _, boundary := range selected.Boundaries() {
		if boundary.Component == subject.identity.String() && boundary.Direction == boundaryDirection(direction) {
			found = append(found, boundary)
		}
	}
	if len(found) != 1 {
		return fmt.Errorf("Provider boundary count = %d, want one", len(found))
	}
	boundary := found[0]
	if boundary.Kind != plan.ProviderBoundary || boundary.Scheme != scheme || boundary.ReferenceFingerprint != reference.Fingerprint().String() {
		return fmt.Errorf("Provider boundary identity = %#v", boundary)
	}
	if !equalCapabilities(boundary.Available, available.Values()) || !equalCapabilities(boundary.Effective, available.Values()) || !equalCapabilities(boundary.Selected, selection.Capabilities()) || !boundary.Spool.IsZero() {
		return fmt.Errorf("Provider capability projection = available %v effective %v selected %v spool %#v", boundary.Available, boundary.Effective, boundary.Selected, boundary.Spool)
	}
	return nil
}

func boundaryDirection(direction access.Direction) plan.BoundaryDirection {
	if direction == access.SourceDirection {
		return plan.InputBoundary
	}
	return plan.OutputBoundary
}

func equalCapabilities(left, right []access.Capability) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func accessReadJob(reference access.Reference) (job.Job, error) {
	boundary, err := job.InputFromReference(reference)
	if err != nil {
		return job.Job{}, err
	}
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("testkit-access-read", plugin.IdentityOf[accessReadPassID](), config.NewPatch()),
			job.NewNode("testkit-access-sink", plugin.IdentityOf[accessReadSinkID](), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("testkit-access-read", "out"), job.At("testkit-access-sink", "in"))},
	)
	if err != nil {
		return job.Job{}, err
	}
	return job.New([]job.Input{boundary}, nil, graph)
}

func accessWriteJob(reference access.Reference) (job.Job, error) {
	boundary, err := job.OutputToReference(reference)
	if err != nil {
		return job.Job{}, err
	}
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("testkit-access-source", plugin.IdentityOf[accessWriteSourceID](), config.NewPatch()),
			job.NewNode("testkit-access-write", plugin.IdentityOf[accessWritePassID](), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("testkit-access-source", "out"), job.At("testkit-access-write", "in"))},
	)
	if err != nil {
		return job.Job{}, err
	}
	return job.New(nil, []job.Output{boundary}, graph)
}

func accessReadFixture(requirements access.Requirements, output recorder[buffer.Handle], state *lifecycleState) plugin.Definition {
	schema := accessFixtureSchema()
	passShape := flow.NewShape([]flow.Port{flow.In("in", access.Bytes())}, []flow.Port{flow.Out("out", access.Bytes())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", access.Bytes())}, nil)
	pass := plugin.NewComponent[accessReadPassID](plugin.Descriptor{DisplayName: "testkit Access read pass"}, schema,
		plugin.WithSpec(plugin.Spec[accessFixtureConfig, accessFixturePlan, stream.Descriptor]{
			Shape: plugin.StaticShape[accessFixtureConfig](passShape),
			Compile: func(_ plugin.CompileContext, _ accessFixtureConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[accessFixturePlan, stream.Descriptor], error) {
				input, ok := inputs.One("in")
				if !ok {
					return plugin.Compiled[accessFixturePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("testkit.access.input"))}}, nil
				}
				return plugin.Compiled[accessFixturePlan, stream.Descriptor]{
					Plan:         accessFixturePlan{shape: passShape.Clone()},
					Outputs:      flow.NewDescriptors(flow.Describe("out", input)),
					Finalization: plugin.RequiresFinalization,
				}, nil
			},
			Open: func(plugin.OpenContext, accessFixturePlan) (flow.Operator, error) {
				state.sourceOpen.Add(1)
				return &accessReadPassOperator{shape: passShape.Clone(), state: state}, nil
			},
			Finalizes: true,
		}),
		plugin.WithProcessor("in", access.Bytes(), "out", access.Bytes()),
		mediaformat.Read(accessFixtureFormat(), requirements),
	)
	sink := plugin.NewComponent[accessReadSinkID](plugin.Descriptor{DisplayName: "testkit Access read sink"}, schema,
		plugin.WithSpec(plugin.Spec[accessFixtureConfig, accessFixturePlan, stream.Descriptor]{
			Shape: plugin.StaticShape[accessFixtureConfig](sinkShape),
			Compile: func(_ plugin.CompileContext, _ accessFixtureConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[accessFixturePlan, stream.Descriptor], error) {
				if _, ok := inputs.One("in"); !ok {
					return plugin.Compiled[accessFixturePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("testkit.access.output"))}}, nil
				}
				return plugin.Compiled[accessFixturePlan, stream.Descriptor]{Plan: accessFixturePlan{shape: sinkShape.Clone()}}, nil
			},
			Open: func(plugin.OpenContext, accessFixturePlan) (flow.Operator, error) {
				state.sinkOpen.Add(1)
				return &fixtureWriter[buffer.Handle]{shape: sinkShape.Clone(), output: output, state: state}, nil
			},
		}),
		plugin.WithWriter("in", access.Bytes()),
	)
	return plugin.Define[accessFixturePluginID](plugin.Descriptor{DisplayName: "testkit Access fixtures", Version: "1"}, pass, sink)
}

func accessWriteFixture(requirements access.Requirements, input *Fixture[buffer.Handle], state *lifecycleState) plugin.Definition {
	schema := accessFixtureSchema()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", access.Bytes())})
	passShape := flow.NewShape([]flow.Port{flow.In("in", access.Bytes())}, []flow.Port{flow.Out("out", access.Writes())})
	source := plugin.NewComponent[accessWriteSourceID](plugin.Descriptor{DisplayName: "testkit Access write source"}, schema,
		plugin.WithSpec(plugin.Spec[accessFixtureConfig, accessFixturePlan, stream.Descriptor]{
			Shape: plugin.StaticShape[accessFixtureConfig](sourceShape),
			Compile: func(plugin.CompileContext, accessFixtureConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[accessFixturePlan, stream.Descriptor], error) {
				return plugin.Compiled[accessFixturePlan, stream.Descriptor]{Plan: accessFixturePlan{shape: sourceShape.Clone()}, Outputs: flow.NewDescriptors(flow.Describe("out", input.descriptor))}, nil
			},
			Open: func(plugin.OpenContext, accessFixturePlan) (flow.Operator, error) {
				state.sourceOpen.Add(1)
				return &fixtureReader[buffer.Handle]{shape: sourceShape.Clone(), input: input, state: state}, nil
			},
		}),
		plugin.WithReader("out", access.Bytes()),
	)
	pass := plugin.NewComponent[accessWritePassID](plugin.Descriptor{DisplayName: "testkit Access write pass"}, schema,
		plugin.WithSpec(plugin.Spec[accessFixtureConfig, accessFixturePlan, stream.Descriptor]{
			Shape: plugin.StaticShape[accessFixtureConfig](passShape),
			Compile: func(_ plugin.CompileContext, _ accessFixtureConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[accessFixturePlan, stream.Descriptor], error) {
				input, ok := inputs.One("in")
				if !ok {
					return plugin.Compiled[accessFixturePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("testkit.access.input"))}}, nil
				}
				output, err := stream.NewDescriptor(input.ID(), access.Writes().Identity(), access.CarrierTimeBase(), property.New())
				if err != nil {
					return plugin.Compiled[accessFixturePlan, stream.Descriptor]{}, err
				}
				return plugin.Compiled[accessFixturePlan, stream.Descriptor]{Plan: accessFixturePlan{shape: passShape.Clone()}, Outputs: flow.NewDescriptors(flow.Describe("out", output.WithMetadata(input.Metadata())))}, nil
			},
			Open: func(plugin.OpenContext, accessFixturePlan) (flow.Operator, error) {
				state.sinkOpen.Add(1)
				return &accessWritePassOperator{shape: passShape.Clone(), state: state}, nil
			},
		}),
		plugin.WithProcessor("in", access.Bytes(), "out", access.Writes()),
		mediaformat.Write(accessFixtureFormat(), requirements),
	)
	return plugin.Define[accessFixturePluginID](plugin.Descriptor{DisplayName: "testkit Access fixtures", Version: "1"}, source, pass)
}

func accessFixtureSchema() config.Schema[accessFixtureConfig] {
	return config.Struct[accessFixtureConfigID](func() accessFixtureConfig { return accessFixtureConfig{} }).Version("1").Build()
}

func accessFixtureFormat() mediaformat.Format {
	value, err := mediaformat.Define[accessFixtureFormatID](nil)
	if err != nil {
		panic(err)
	}
	return value
}

type accessReadPassOperator struct {
	shape  flow.Shape
	state  *lifecycleState
	closed bool
}

func (o *accessReadPassOperator) Ports() flow.Shape { return o.shape.Clone() }
func (o *accessReadPassOperator) Process(ctx context.Context, input flow.Input[buffer.Handle], output flow.Emitter[buffer.Handle]) error {
	owned := input.Take()
	if err := output.Emit(ctx, flow.NewInput(owned.Value(), access.Bytes())); err != nil {
		owned.Release()
		return err
	}
	return nil
}
func (*accessReadPassOperator) Flush(context.Context, flow.Emitter[buffer.Handle]) error { return nil }
func (o *accessReadPassOperator) Finalize(context.Context) error {
	o.state.eof.Add(1)
	return nil
}
func (o *accessReadPassOperator) Close() error {
	if !o.closed {
		o.closed = true
		o.state.sourceClose.Add(1)
	}
	return nil
}

type accessWritePassOperator struct {
	shape  flow.Shape
	state  *lifecycleState
	closed bool
}

func (o *accessWritePassOperator) Ports() flow.Shape { return o.shape.Clone() }
func (o *accessWritePassOperator) Process(ctx context.Context, input flow.Input[buffer.Handle], output flow.Emitter[access.Write]) error {
	owned := input.Take()
	write, err := access.Append(owned.Value())
	if err != nil {
		owned.Release()
		return err
	}
	item := flow.NewInput(write, access.Writes())
	if err := output.Emit(ctx, item); err != nil {
		item.Drop()
		return err
	}
	return nil
}
func (*accessWritePassOperator) Flush(context.Context, flow.Emitter[access.Write]) error { return nil }
func (o *accessWritePassOperator) Close() error {
	if !o.closed {
		o.closed = true
		o.state.sinkClose.Add(1)
	}
	return nil
}
