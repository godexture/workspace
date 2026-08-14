package file

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

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
	"github.com/godexture/godec/resource"
)

type (
	spoolPluginID struct{}
	spoolPassID   struct{}
	spoolFormatID struct{}
	spoolOperator struct {
		shape     flow.Shape
		buffers   *buffer.Allocator
		finalized bool
		flushed   bool
	}
)

var spoolInput = []byte("0000payload")
var spoolPatch = []byte("SIZE")

func (o *spoolOperator) Ports() flow.Shape { return o.shape.Clone() }
func (*spoolOperator) Close() error        { return nil }

func (o *spoolOperator) Process(ctx context.Context, input *flow.Item[buffer.Handle], output flow.Emitter[access.Write]) error {
	defer input.Drop()
	var item flow.Item[access.Write]
	defer item.Drop()
	if err := flow.Transfer(input, &item, output, access.Append); err != nil {
		return err
	}
	return output.Emit(ctx, &item)
}

func (o *spoolOperator) Finalize(context.Context) error {
	o.finalized = true
	return nil
}

func (o *spoolOperator) Flush(ctx context.Context, output flow.Emitter[access.Write]) error {
	if !o.finalized {
		return errors.New("positioned fixture must be finalized before flush")
	}
	if o.flushed {
		return nil
	}
	payload, err := o.buffers.FromBytes(spoolPatch, 1)
	if err != nil {
		return err
	}
	write, err := access.Patch(0, payload)
	if err != nil {
		payload.Release()
		return err
	}
	item := flow.NewItem(write, access.Writes(), &testDomain)
	if err := output.Emit(ctx, &item); err != nil {
		item.Drop()
		return err
	}
	o.flushed = true
	return nil
}

type sequentialOutputState struct {
	mu          sync.Mutex
	output      []byte
	selected    []access.Capability
	writeErr    error
	commitErr   error
	aborted     int
	closed      int
	partialSize int
}

type sequentialOutputSession struct {
	state     *sequentialOutputState
	staged    []byte
	committed bool
}

func (*sequentialOutputSession) Capabilities() access.Capabilities {
	value, _ := access.NewCapabilities(access.SequentialWrite)
	return value
}

func (s *sequentialOutputSession) Write(_ context.Context, source []byte) (int, error) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if s.state.writeErr != nil {
		return 0, s.state.writeErr
	}
	size := len(source)
	if s.state.partialSize > 0 {
		size = min(size, s.state.partialSize)
	}
	s.staged = append(s.staged, source[:size]...)
	return size, nil
}

func (*sequentialOutputSession) Flush(context.Context) error         { return nil }
func (*sequentialOutputSession) Sync(context.Context) error          { return nil }
func (*sequentialOutputSession) PrepareCommit(context.Context) error { return nil }

func (s *sequentialOutputSession) Commit(context.Context) error {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if s.state.commitErr != nil {
		return s.state.commitErr
	}
	s.state.output = append([]byte(nil), s.staged...)
	s.committed = true
	return nil
}

func (s *sequentialOutputSession) Abort(context.Context) error {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if !s.committed {
		s.staged = nil
		s.state.aborted++
	}
	return nil
}

func (s *sequentialOutputSession) Close() error {
	s.state.mu.Lock()
	s.state.closed++
	s.staged = nil
	s.state.mu.Unlock()
	return nil
}

func TestPositionedOutputSpoolsToSequentialSinkWithExplicitPlanProjection(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.bin")
	directPath := filepath.Join(directory, "direct.bin")
	if err := os.WriteFile(inputPath, spoolInput, 0o600); err != nil {
		t.Fatal(err)
	}
	policy := spoolPolicy(t, access.MemorySpool, 1<<20)
	directHost, err := host.New(host.Plugins(plugin.NewSet(Plugin(), spoolFixture())))
	if err != nil {
		t.Fatal(err)
	}
	directRequest := spoolOutputJob(t, inputPath, fileReference(t, directPath), policy)
	directPlan, err := directHost.Plan(t.Context(), directRequest)
	if err != nil {
		t.Fatal(err)
	}
	if outputBoundary(t, directPlan).Spool.Valid() {
		t.Fatal("direct random-write file output unexpectedly selected a spool")
	}
	result, err := directHost.Run(t.Context(), directRequest)
	if err != nil || !result.Succeeded() {
		t.Fatalf("direct positioned result = %#v, %v", result, err)
	}
	direct, err := os.ReadFile(directPath)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]byte(nil), spoolPatch...), spoolInput[len(spoolPatch):]...)
	if !bytes.Equal(direct, want) {
		t.Fatalf("direct positioned output = %q, want %q", direct, want)
	}

	for _, storage := range []access.SpoolStorage{access.MemorySpool, access.DiskSpool} {
		t.Run(spoolStorageLabel(storage), func(t *testing.T) {
			state := &sequentialOutputState{output: []byte("old"), partialSize: 7}
			instance := sequentialOutputHost(t, state)
			request := spoolOutputJob(t, inputPath, sequenceReference(t), spoolPolicy(t, storage, 1<<20))
			prepared, err := instance.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			boundary := outputBoundary(t, prepared.Plan())
			if len(boundary.Available) != 1 || boundary.Available[0] != access.SequentialWrite ||
				len(boundary.Effective) != 2 || boundary.Effective[0] != access.RandomWrite || boundary.Effective[1] != access.SequentialWrite ||
				len(boundary.Selected) != 1 || boundary.Selected[0] != access.RandomWrite || !boundary.Spool.Valid() || boundary.Spool.Storage() != storage || !boundary.Spool.FinalCopy() {
				t.Fatalf("spooled boundary = %#v", boundary)
			}
			result, err := prepared.Run(t.Context())
			if err != nil || !result.Succeeded() {
				t.Fatalf("spooled positioned result = %#v, %v", result, err)
			}
			state.mu.Lock()
			output := append([]byte(nil), state.output...)
			selected := append([]access.Capability(nil), state.selected...)
			closed := state.closed
			state.mu.Unlock()
			if !bytes.Equal(output, direct) {
				t.Fatal("spooled positioned output differs from direct random-write output")
			}
			if len(selected) != 1 || selected[0] != access.SequentialWrite || closed != 1 {
				t.Fatalf("underlying selection/close = %v/%d", selected, closed)
			}
		})
	}
}

func TestPositionedSpoolFailuresDoNotPublishSequentialTarget(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.bin")
	if err := os.WriteFile(inputPath, spoolInput, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		maximum   resource.Bytes
		configure func(*sequentialOutputState)
		cancel    bool
	}{
		{name: "quota", maximum: 8},
		{name: "final copy", maximum: 1 << 20, configure: func(state *sequentialOutputState) { state.writeErr = errors.New("copy failed") }},
		{name: "commit", maximum: 1 << 20, configure: func(state *sequentialOutputState) { state.commitErr = errors.New("commit failed") }},
		{name: "cancel", maximum: 1 << 20, cancel: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := &sequentialOutputState{output: []byte("old")}
			if test.configure != nil {
				test.configure(state)
			}
			instance := sequentialOutputHost(t, state)
			request := spoolOutputJob(t, inputPath, sequenceReference(t), spoolPolicy(t, access.MemorySpool, test.maximum))
			prepared, err := instance.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			if test.cancel {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			result, err := prepared.Run(ctx)
			if err == nil || result.Succeeded() {
				t.Fatalf("failing spool result = %#v, %v", result, err)
			}
			state.mu.Lock()
			output := append([]byte(nil), state.output...)
			closed := state.closed
			state.mu.Unlock()
			if string(output) != "old" || closed != 1 {
				t.Fatalf("failed spool published/retained target = %q, closed %d", output, closed)
			}
		})
	}
}

func sequentialOutputHost(t *testing.T, state *sequentialOutputState) *host.Host {
	t.Helper()
	capabilities, err := access.NewCapabilities(access.SequentialWrite)
	if err != nil {
		t.Fatal(err)
	}
	acquire := func(_ context.Context, _ access.Reference, selection access.Selection) (access.Session, error) {
		state.mu.Lock()
		state.selected = selection.Capabilities()
		state.mu.Unlock()
		return &sequentialOutputSession{state: state}, nil
	}
	sink := sinkComponentWith(
		plugin.Descriptor{DisplayName: "Sequential fixture sink"},
		access.Sink("sequence", capabilities, access.AtomicReplace, acquire),
	)
	set := plugin.NewSet(Plugin(), spoolFixture()).Override(SinkIdentity(), sink)
	instance, err := host.New(host.Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	return instance
}

func spoolOutputJob(t *testing.T, inputPath string, outputReference access.Reference, policy job.Policy) job.Job {
	t.Helper()
	graph, err := job.NewGraph(
		[]job.Node{job.NewNode("positioned", plugin.IdentityOf[spoolPassID](), config.NewPatch())},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	input, err := job.InputFromReference(fileReference(t, inputPath))
	if err != nil {
		t.Fatal(err)
	}
	output, err := job.OutputToReference(outputReference)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New([]job.Input{input}, []job.Output{output}, graph, job.WithPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func spoolFixture() plugin.Definition {
	shape := flow.NewShape(
		[]flow.Port{flow.In("bytes", access.Bytes())},
		[]flow.Port{flow.Out("writes", access.Writes())},
	)
	spec := plugin.Spec[lifecycleConfig, lifecyclePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[lifecycleConfig](shape),
		Compile: func(_ plugin.CompileContext, _ lifecycleConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[lifecyclePlan, stream.Descriptor], error) {
			input, ok := inputs.One("bytes")
			if !ok {
				return plugin.Compiled[lifecyclePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{
					plugin.Require("bytes", plugin.ConditionNeed[stream.Descriptor]("spool.input")),
				}}, nil
			}
			output, err := stream.NewDescriptor(input.ID(), access.Writes().Identity(), access.CarrierTimeBase(), property.New())
			if err != nil {
				return plugin.Compiled[lifecyclePlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[lifecyclePlan, stream.Descriptor]{
				Plan:         lifecyclePlan{shape: shape.Clone()},
				Outputs:      flow.NewDescriptors(flow.Describe("writes", output.WithMetadata(input.Metadata()))),
				Resources:    resource.Request{Memory: resource.Bytes(len(spoolPatch))},
				Finalization: plugin.RequiresFinalization,
			}, nil
		},
		Open: func(ctx plugin.OpenContext, _ lifecyclePlan) (flow.Operator, error) {
			if ctx.Buffers() == nil {
				return nil, errors.New("positioned fixture requires a payload allocator")
			}
			return &spoolOperator{shape: shape.Clone(), buffers: ctx.Buffers()}, nil
		},
		Finalizes: true,
	}
	formatValue, err := mediaformat.Define[spoolFormatID](nil)
	if err != nil {
		panic(err)
	}
	component := plugin.NewComponent[spoolPassID](
		plugin.Descriptor{DisplayName: "Positioned write fixture"},
		lifecycleSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("bytes", access.Bytes(), "writes", access.Writes()),
		mediaformat.Read(formatValue, access.NewRequirements(access.AllOf(access.SequentialRead))),
		mediaformat.Write(formatValue, access.NewRequirements(access.AllOf(access.RandomWrite))),
	)
	return plugin.Define[spoolPluginID](plugin.Descriptor{DisplayName: "Positioned write fixture", Version: "1"}, component)
}

func spoolPolicy(t *testing.T, storage access.SpoolStorage, maximum resource.Bytes) job.Policy {
	t.Helper()
	policy, ok := job.PolicyFor(job.Fast)
	if !ok {
		t.Fatal("Fast policy is unavailable")
	}
	policy.Resources.AllowSpool = true
	policy.Resources.SpoolMaxBytes = maximum
	policy.Resources.SpoolStorage = storage
	if !policy.Valid() {
		t.Fatalf("spool policy = %#v", policy.Resources)
	}
	return policy
}

func sequenceReference(t *testing.T) access.Reference {
	t.Helper()
	reference, err := access.Parse("sequence:output")
	if err != nil {
		t.Fatal(err)
	}
	return reference
}

func outputBoundary(t *testing.T, value plan.Plan) plan.Boundary {
	t.Helper()
	for _, boundary := range value.Boundaries() {
		if boundary.Direction == plan.OutputBoundary {
			return boundary
		}
	}
	t.Fatal("Plan has no output boundary")
	return plan.Boundary{}
}

func spoolStorageLabel(value access.SpoolStorage) string {
	if value == access.MemorySpool {
		return "memory"
	}
	return "disk"
}
