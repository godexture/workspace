package testkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type ownershipPluginID struct{}
type ownershipConfigID struct{}
type ownershipSchemaID struct{}
type ownershipSourceID struct{}
type ownershipSinkID struct{}

type ownershipConfig struct{}
type ownershipPlan struct{ shape flow.Shape }
type ownershipHandle struct{ closed atomic.Int32 }

var ownershipType = schema.Define[ownershipSchemaID](schema.Traits[int]{})

func (h *ownershipHandle) Close() error {
	h.closed.Add(1)
	return nil
}

func verifyAccessOwnership(t testing.TB) {
	t.Helper()
	for _, ownership := range []access.Ownership{access.Owned, access.Borrowed} {
		ownership := ownership
		runNamed(t, directionNameForOwnership(ownership), func(child testing.TB) {
			executeCase(child, plugin.IdentityOf[ownershipSourceID](), "", func() (*scenarioCore, error) {
				return newOwnershipScenario(ownership)
			})
		})
	}
}

func directionNameForOwnership(ownership access.Ownership) string {
	if ownership == access.Owned {
		return "owned"
	}
	return "borrowed"
}

func newOwnershipScenario(ownership access.Ownership) (*scenarioCore, error) {
	state := &lifecycleState{}
	var received atomic.Int32
	sourceHandle := &ownershipHandle{}
	sinkHandle := &ownershipHandle{}
	definition := ownershipDefinition(state, sourceHandle, sinkHandle, &received)
	instance, err := host.New(host.Plugins(plugin.NewSet(definition)))
	if err != nil {
		return nil, err
	}
	sourceAdaptor, err := job.NewAdaptor(plugin.IdentityOf[ownershipSourceID](), config.NewPatch())
	if err != nil {
		return nil, err
	}
	sinkAdaptor, err := job.NewAdaptor(plugin.IdentityOf[ownershipSinkID](), config.NewPatch())
	if err != nil {
		return nil, err
	}
	var input job.Input
	var output job.Output
	if ownership == access.Owned {
		input, err = job.InputFromSource(access.Own(sourceHandle), sourceAdaptor)
		if err == nil {
			output, err = job.OutputToSink(access.Own(sinkHandle), sinkAdaptor)
		}
	} else {
		input, err = job.InputFromSource(access.Borrow(sourceHandle), sourceAdaptor)
		if err == nil {
			output, err = job.OutputToSink(access.Borrow(sinkHandle), sinkAdaptor)
		}
	}
	if err != nil {
		return nil, err
	}
	request, err := job.New([]job.Input{input}, []job.Output{output}, job.Graph{})
	if err != nil {
		return nil, err
	}
	wantClose := int32(0)
	if ownership == access.Owned {
		wantClose = 1
	}
	return &scenarioCore{
		host:  instance,
		job:   request,
		state: state,
		finish: func() error {
			if received.Load() != 7 {
				return fmt.Errorf("direct sink received %d, want 7", received.Load())
			}
			return nil
		},
		cleanup: func() error {
			if sourceHandle.closed.Load() != wantClose || sinkHandle.closed.Load() != wantClose {
				return fmt.Errorf("%s direct Close counts = source %d sink %d, want %d", directionNameForOwnership(ownership), sourceHandle.closed.Load(), sinkHandle.closed.Load(), wantClose)
			}
			return nil
		},
	}, nil
}

func ownershipDefinition(state *lifecycleState, sourceHandle, sinkHandle *ownershipHandle, received *atomic.Int32) plugin.Definition {
	configuration := config.Struct[ownershipConfigID](func() ownershipConfig { return ownershipConfig{} }).Version("1").Build()
	descriptor := stream.MustDescriptor("testkit-direct", ownershipType.Identity(), timing.MustBase(1, 1), property.New())
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", ownershipType)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", ownershipType)}, nil)
	source := plugin.NewComponent[ownershipSourceID](plugin.Descriptor{DisplayName: "testkit direct source"}, configuration,
		plugin.WithSpec(plugin.Spec[ownershipConfig, ownershipPlan, stream.Descriptor]{
			Shape: plugin.StaticShape[ownershipConfig](sourceShape),
			Compile: func(plugin.CompileContext, ownershipConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[ownershipPlan, stream.Descriptor], error) {
				return plugin.Compiled[ownershipPlan, stream.Descriptor]{Plan: ownershipPlan{shape: sourceShape.Clone()}, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor))}, nil
			},
			Open: func(ctx plugin.OpenContext, plan ownershipPlan) (flow.Operator, error) {
				opening, ok := plugin.Boundary[access.Direct[*ownershipHandle]](ctx)
				if !ok || !opening.Valid() || opening.Value() != sourceHandle {
					return nil, errors.New("testkit direct source opening is invalid")
				}
				state.sourceOpen.Add(1)
				return &ownershipSource{shape: plan.shape.Clone(), state: state}, nil
			},
		}),
		plugin.WithReader("out", ownershipType),
	)
	sink := plugin.NewComponent[ownershipSinkID](plugin.Descriptor{DisplayName: "testkit direct sink"}, configuration,
		plugin.WithSpec(plugin.Spec[ownershipConfig, ownershipPlan, stream.Descriptor]{
			Shape: plugin.StaticShape[ownershipConfig](sinkShape),
			Compile: func(_ plugin.CompileContext, _ ownershipConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[ownershipPlan, stream.Descriptor], error) {
				if _, ok := inputs.One("in"); !ok {
					return plugin.Compiled[ownershipPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("testkit.direct.input"))}}, nil
				}
				return plugin.Compiled[ownershipPlan, stream.Descriptor]{Plan: ownershipPlan{shape: sinkShape.Clone()}}, nil
			},
			Open: func(ctx plugin.OpenContext, plan ownershipPlan) (flow.Operator, error) {
				opening, ok := plugin.Boundary[access.Direct[*ownershipHandle]](ctx)
				if !ok || !opening.Valid() || opening.Value() != sinkHandle {
					return nil, errors.New("testkit direct sink opening is invalid")
				}
				state.sinkOpen.Add(1)
				return &ownershipSink{shape: plan.shape.Clone(), state: state, received: received}, nil
			},
		}),
		plugin.WithWriter("in", ownershipType),
	)
	return plugin.Define[ownershipPluginID](plugin.Descriptor{DisplayName: "testkit direct ownership", Version: "1"}, source, sink)
}

type ownershipSource struct {
	shape  flow.Shape
	state  *lifecycleState
	read   bool
	closed bool
}

func (o *ownershipSource) Ports() flow.Shape { return o.shape.Clone() }
func (o *ownershipSource) Read(ctx context.Context) (flow.Input[int], error) {
	if err := ctx.Err(); err != nil {
		return flow.Input[int]{}, err
	}
	if o.read {
		o.state.eof.Add(1)
		return flow.Input[int]{}, io.EOF
	}
	o.read = true
	return flow.NewInput(7, ownershipType), nil
}
func (o *ownershipSource) Close() error {
	if !o.closed {
		o.closed = true
		o.state.sourceClose.Add(1)
	}
	return nil
}

type ownershipSink struct {
	shape    flow.Shape
	state    *lifecycleState
	received *atomic.Int32
	closed   bool
}

func (o *ownershipSink) Ports() flow.Shape { return o.shape.Clone() }
func (o *ownershipSink) Write(_ context.Context, input flow.Input[int]) error {
	if !input.Valid() {
		return errors.New("testkit direct sink received an invalid item")
	}
	o.received.Store(int32(input.Value()))
	input.Drop()
	return nil
}
func (o *ownershipSink) Close() error {
	if !o.closed {
		o.closed = true
		o.state.sinkClose.Add(1)
	}
	return nil
}
