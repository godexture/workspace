package plan

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/endpoint"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type planConfigID struct{}
type planSchemaID struct{}
type planUnit struct{}

type planConfig struct {
	Mode  string
	Token config.SecretValue[string]
}

func planConfiguration() config.Schema[planConfig] {
	return config.Struct[planConfigID](func() planConfig {
		return planConfig{Mode: "copy", Token: config.NewSecret("default-secret")}
	}).
		Version("1").
		AddField(config.Field("mode", func(value *planConfig) *string { return &value.Mode }, config.String())).
		AddField(config.Field("token", func(value *planConfig) *config.SecretValue[string] { return &value.Token }, config.SecretCodec(config.String()))).
		Build()
}

func TestPlanBoundaryIsImmutableAndCanonicalWithoutDisplayReference(t *testing.T) {
	description := testDescription(t)
	description.Boundaries = []Boundary{{
		Direction:            InputBoundary,
		Kind:                 ProviderBoundary,
		Choice:               0,
		Node:                 "source",
		Port:                 "out",
		Component:            "fixture.source",
		Scheme:               "memory",
		Reference:            "memory://first/redacted",
		ReferenceFingerprint: "canonical-reference-fingerprint",
		Available:            []access.Capability{access.RandomRead, access.SequentialRead},
		Effective:            []access.Capability{access.RandomRead, access.SequentialRead},
		Selected:             []access.Capability{access.SequentialRead},
	}}
	first, err := New(description)
	if err != nil {
		t.Fatal(err)
	}

	display := testDescription(t)
	display.Boundaries = description.Boundaries
	display.Boundaries[0].Reference = "memory://renamed/redacted"
	second, err := New(display)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint() != second.Fingerprint() || first.ExecutionSignature() != second.ExecutionSignature() {
		t.Fatal("redacted reference display changed canonical Plan identity")
	}

	boundaries := first.Boundaries()
	boundaries[0].Available[0] = access.StableSize
	boundaries[0].Effective[0] = access.StableSize
	if first.Boundaries()[0].Available[0] != access.RandomRead || first.Boundaries()[0].Effective[0] != access.RandomRead {
		t.Fatal("Plan exposed mutable boundary capability storage")
	}

	changed := testDescription(t)
	changed.Boundaries = description.Boundaries
	changed.Boundaries[0].ReferenceFingerprint = "different-canonical-reference"
	third, err := New(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExecutionSignature() == third.ExecutionSignature() {
		t.Fatal("private reference identity did not affect execution signature")
	}
}

func TestPlanBoundaryUsesActualDirectionalDataAnchor(t *testing.T) {
	description := testDescription(t)
	description.Nodes[0].Outputs = append(description.Nodes[0].Outputs,
		PortDescriptor{Port: "out", Descriptor: description.Nodes[0].Outputs[0].Descriptor},
		PortDescriptor{Port: "alternate", Descriptor: description.Nodes[0].Outputs[0].Descriptor},
	)
	description.Boundaries = []Boundary{{
		Direction:            InputBoundary,
		Kind:                 ProviderBoundary,
		Choice:               0,
		Node:                 "source",
		Port:                 "out",
		Component:            "fixture.provider",
		Scheme:               "memory",
		Reference:            "memory:redacted",
		ReferenceFingerprint: "anchor-reference",
		Available:            []access.Capability{access.SequentialRead},
		Effective:            []access.Capability{access.SequentialRead},
		Selected:             []access.Capability{access.SequentialRead},
	}}
	first, err := New(description)
	if err != nil {
		t.Fatal(err)
	}
	if first.Boundaries()[0].Component == description.Nodes[0].Component {
		t.Fatal("fixture did not distinguish provider and data-anchor identities")
	}

	changed := description
	changed.Boundaries = append([]Boundary(nil), description.Boundaries...)
	changed.Boundaries[0].Port = "alternate"
	second, err := New(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExecutionSignature() == second.ExecutionSignature() {
		t.Fatal("boundary data anchor did not affect execution identity")
	}

	for name, boundary := range map[string]Boundary{
		"input names an input": {
			Direction:            InputBoundary,
			Kind:                 ProviderBoundary,
			Choice:               0,
			Node:                 "sink",
			Port:                 "in",
			Component:            "fixture.provider",
			Scheme:               "memory",
			Reference:            "memory:redacted",
			ReferenceFingerprint: "anchor-reference",
			Available:            []access.Capability{access.SequentialRead},
			Effective:            []access.Capability{access.SequentialRead},
			Selected:             []access.Capability{access.SequentialRead},
		},
		"output names an output": {
			Direction: OutputBoundary,
			Kind:      EndpointBoundary,
			Choice:    0,
			Node:      "source",
			Port:      "out",
			Component: "fixture.endpoint",
			Topology:  endpoint.FiniteStatic,
			Mode:      endpoint.Offline,
		},
	} {
		t.Run(name, func(t *testing.T) {
			invalid := description
			invalid.Boundaries = []Boundary{boundary}
			if _, err := New(invalid); err == nil {
				t.Fatal("boundary with wrong data-port direction was accepted")
			}
		})
	}
}

func TestPlanSpoolProjectionAffectsExecutionIdentity(t *testing.T) {
	spool, err := access.NewSpoolSpec(4096, 0, access.MemorySpool, 0, true, access.AtomicReplace)
	if err != nil {
		t.Fatal(err)
	}
	description := testDescription(t)
	description.RequestedPolicy.Resources.ScratchMaxBytes = 4096
	description.EffectivePolicy.Resources.ScratchMaxBytes = 4096
	description.Scratch = Scratch{Limit: 4096, Reserved: 4096, TemporaryLimit: description.EffectivePolicy.Resources.TemporaryMaxBytes}
	description.Boundaries = []Boundary{{
		Direction:            OutputBoundary,
		Kind:                 ProviderBoundary,
		Choice:               0,
		Node:                 "sink",
		Port:                 "in",
		Component:            "fixture.sink",
		Scheme:               "memory",
		Reference:            "memory:redacted",
		ReferenceFingerprint: "canonical-reference-fingerprint",
		Available:            []access.Capability{access.SequentialWrite},
		Effective:            []access.Capability{access.RandomWrite, access.SequentialWrite},
		Selected:             []access.Capability{access.RandomWrite},
		Spool:                spool,
	}}
	first, err := New(description)
	if err != nil {
		t.Fatal(err)
	}
	changed := testDescription(t)
	changed.Boundaries = description.Boundaries
	changed.RequestedPolicy.Resources.ScratchMaxBytes = 8192
	changed.EffectivePolicy.Resources.ScratchMaxBytes = 8192
	changed.Scratch = Scratch{Limit: 8192, Reserved: 8192, TemporaryLimit: changed.EffectivePolicy.Resources.TemporaryMaxBytes}
	changed.Boundaries[0].Spool, err = access.NewSpoolSpec(8192, 0, access.MemorySpool, 0, true, access.AtomicReplace)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExecutionSignature() == second.ExecutionSignature() {
		t.Fatal("spool quota did not affect execution identity")
	}
}

func TestPlanScratchProjectionIsCanonicalAndValidated(t *testing.T) {
	description := testDescription(t)
	description.RequestedPolicy.Resources.ScratchMaxBytes = 8
	description.EffectivePolicy.Resources.ScratchMaxBytes = 8
	description.Nodes[0].Scratch = 2
	description.Scratch = Scratch{Limit: 8, Reserved: 2, TemporaryLimit: description.EffectivePolicy.Resources.TemporaryMaxBytes}
	first, err := New(description)
	if err != nil {
		t.Fatal(err)
	}
	if got := first.Scratch(); got != (Scratch{Limit: 8, Reserved: 2, TemporaryLimit: description.EffectivePolicy.Resources.TemporaryMaxBytes}) {
		t.Fatalf("scratch projection = %#v", got)
	}
	changed := description
	changed.Nodes = append([]Node(nil), description.Nodes...)
	changed.Nodes[0].Scratch = 3
	changed.Scratch.Reserved = 3
	second, err := New(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExecutionSignature() == second.ExecutionSignature() {
		t.Fatal("node scratch claim did not affect execution identity")
	}
	invalid := description
	invalid.Scratch.Reserved = 1
	if _, err := New(invalid); err == nil {
		t.Fatal("mismatched scratch reservation was accepted")
	}
}

func TestPlanScratchAggregatesNodeAndSpoolClaims(t *testing.T) {
	spool, err := access.NewSpoolSpec(4, 0, access.MemorySpool, 0, true, access.AtomicReplace)
	if err != nil {
		t.Fatal(err)
	}
	description := testDescription(t)
	description.RequestedPolicy.Resources.ScratchMaxBytes = 6
	description.EffectivePolicy.Resources.ScratchMaxBytes = 6
	description.Nodes[0].Scratch = 2
	description.Scratch = Scratch{Limit: 6, Reserved: 6, TemporaryLimit: description.EffectivePolicy.Resources.TemporaryMaxBytes}
	description.Boundaries = []Boundary{{
		Direction:            OutputBoundary,
		Kind:                 ProviderBoundary,
		Choice:               0,
		Node:                 "sink",
		Port:                 "in",
		Component:            "fixture.sink",
		Scheme:               "memory",
		Reference:            "memory:redacted",
		ReferenceFingerprint: "scratch-spool",
		Available:            []access.Capability{access.SequentialWrite},
		Effective:            []access.Capability{access.RandomWrite, access.SequentialWrite},
		Selected:             []access.Capability{access.RandomWrite},
		Spool:                spool,
	}}
	if _, err := New(description); err != nil {
		t.Fatalf("node and spool aggregate = %v", err)
	}
	description.Nodes[0].Scratch = resource.Bytes(math.MaxInt64)
	description.Scratch = Scratch{Limit: resource.Bytes(math.MaxInt64), Reserved: resource.Bytes(math.MaxInt64)}
	if _, err := New(description); err == nil {
		t.Fatal("node and spool reservation overflow was accepted")
	}
}

func testDescription(t *testing.T) Description {
	t.Helper()
	configuration := planConfiguration()
	resolved, err := configuration.View().Resolve(config.NewPatch().SetText("token", "live-secret"))
	if err != nil {
		t.Fatal(err)
	}
	typ := schema.Define[planSchemaID, planUnit](schema.Traits[planUnit]{Time: func(planUnit) (int64, bool) { return 0, true }})
	descriptor, err := ProjectDescriptor(stream.MustDescriptor("audio", typ.Descriptor(), timing.MustBase(1, 48000), property.New()))
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := job.PolicyFor(job.Fast)
	contract := plugin.DefaultContract()
	contract.Platform.Features = []string{"portable"}
	return Description{
		RequestedPolicy:    policy,
		EffectivePolicy:    policy,
		Budget:             job.DefaultBudget(),
		Usage:              Usage{States: 1, Compiles: 2, FixpointIterations: 1},
		CatalogFingerprint: "catalog",
		Platform:           Platform{OS: "test", Arch: "test", Toolchain: "go-test", Features: []string{"z", "a"}},
		Nodes: []Node{
			{ID: "source", Origin: Requested, Component: "fixture.source", DisplayName: "Source", Variant: "fixture.source#default", Version: "1", Config: resolved.Summary(), Outputs: []PortDescriptor{{Port: "out", Descriptor: descriptor}}, Contract: contract},
			{ID: "sink", Origin: Requested, Component: "fixture.sink", DisplayName: "Sink", Variant: "fixture.sink#default", Version: "1", Config: resolved.Summary(), Inputs: []PortDescriptor{{Port: "in", Descriptor: descriptor}}, Contract: contract},
		},
		Edges:    []Edge{{FromNode: "source", FromPort: "out", ToNode: "sink", ToPort: "in", Origin: Requested}},
		Scratch:  Scratch{Limit: policy.Resources.ScratchMaxBytes, TemporaryLimit: policy.Resources.TemporaryMaxBytes},
		Warnings: []string{"display warning"},
	}
}

func TestProjectDescriptorPreservesUntimedTimelineState(t *testing.T) {
	type carrierID struct{}
	type carrierValue struct{}
	typ := schema.Define[carrierID, carrierValue](schema.Traits[carrierValue]{})
	value := stream.MustDescriptor("bytes", typ.Descriptor(), timing.Base{}, property.New())
	descriptor, err := ProjectDescriptor(value)
	if err != nil {
		t.Fatal(err)
	}
	if !descriptor.Valid() || descriptor.HasTimeline || descriptor.TimeBaseNumerator != 0 || descriptor.TimeBaseDenominator != 0 {
		t.Fatalf("untimed plan descriptor = %#v", descriptor)
	}
}

func TestPlanIsInertImmutableAndRedacted(t *testing.T) {
	description := testDescription(t)
	planned, err := New(description)
	if err != nil {
		t.Fatal(err)
	}
	if !planned.Valid() || planned.Fingerprint().IsZero() || planned.ExecutionSignature().IsZero() {
		t.Fatalf("invalid plan: %#v", planned)
	}
	rendered := fmt.Sprintf("%#v", planned.Description())
	if strings.Contains(rendered, "live-secret") || strings.Contains(rendered, "default-secret") {
		t.Fatalf("secret leaked through Plan: %s", rendered)
	}
	nodes := planned.Nodes()
	nodes[0].Inputs = append(nodes[0].Inputs, PortDescriptor{})
	nodes[0].Contract.Platform.Features[0] = "changed"
	if len(planned.Nodes()[0].Inputs) != 0 || planned.Nodes()[0].Contract.Platform.Features[0] != "portable" {
		t.Fatal("Plan retained mutable projection storage")
	}
	platform := planned.Platform()
	platform.Features[0] = "changed"
	if planned.Platform().Features[0] != "a" {
		t.Fatal("Plan retained mutable platform storage")
	}
}

func TestPlanMappingProjectionIsImmutableAndAffectsIdentity(t *testing.T) {
	description := testDescription(t)
	description.Boundaries = mappingBoundaries()
	description.Mappings = []Mapping{
		{Input: 0, Stream: "video", Output: 0},
		{Input: 0, Stream: "audio", Output: 0},
	}
	planned, err := New(description)
	if err != nil {
		t.Fatal(err)
	}
	got := planned.Mappings()
	if len(got) != 2 || got[0] != description.Mappings[0] || got[1] != description.Mappings[1] {
		t.Fatalf("Plan mappings = %#v, want %#v", got, description.Mappings)
	}
	got[0].Stream = "changed"
	if planned.Mappings()[0].Stream != "video" {
		t.Fatal("Plan exposed mutable mapping storage")
	}
	description.Mappings[0].Stream = "caller changed"
	if planned.Mappings()[0].Stream != "video" {
		t.Fatal("New retained Description mapping storage")
	}
	description.Mappings[0].Stream = "video"
	projected := planned.Description()
	projected.Mappings[0].Stream = "description changed"
	if planned.Mappings()[0].Stream != "video" {
		t.Fatal("Description exposed Plan mapping storage")
	}

	changed := testDescription(t)
	changed.Mappings = []Mapping{{Input: 0, Stream: "audio", Output: 0}}
	changed.Boundaries = description.Boundaries
	other, err := New(changed)
	if err != nil {
		t.Fatal(err)
	}
	if planned.Fingerprint() == other.Fingerprint() || planned.ExecutionSignature() == other.ExecutionSignature() {
		t.Fatal("mapping projection did not affect Plan identity")
	}
	swapped := testDescription(t)
	swapped.Boundaries = description.Boundaries
	swapped.Mappings = []Mapping{description.Mappings[1], description.Mappings[0]}
	reordered, err := New(swapped)
	if err != nil {
		t.Fatal(err)
	}
	if planned.Fingerprint() == reordered.Fingerprint() || planned.ExecutionSignature() == reordered.ExecutionSignature() {
		t.Fatal("mapping sequence order did not affect Plan identity")
	}

	missing := testDescription(t)
	missing.Mappings = description.Mappings
	missing.Boundaries = description.Boundaries[:1]
	if _, err := New(missing); err == nil {
		t.Fatal("mapping with a missing output boundary was accepted")
	}
}

func mappingBoundaries() []Boundary {
	return []Boundary{
		{
			Direction:            InputBoundary,
			Kind:                 ProviderBoundary,
			Choice:               0,
			Node:                 "source",
			Port:                 "out",
			Component:            "fixture.provider",
			Scheme:               "memory",
			Reference:            "memory:redacted",
			ReferenceFingerprint: "mapping-input",
			Available:            []access.Capability{access.SequentialRead},
			Effective:            []access.Capability{access.SequentialRead},
			Selected:             []access.Capability{access.SequentialRead},
		},
		{
			Direction: OutputBoundary,
			Kind:      EndpointBoundary,
			Choice:    0,
			Node:      "sink",
			Port:      "in",
			Component: "fixture.endpoint",
			Topology:  endpoint.FiniteStatic,
			Mode:      endpoint.Offline,
		},
	}
}

func TestPlanRejectsInvalidAndDuplicateMappings(t *testing.T) {
	for name, mappings := range map[string][]Mapping{
		"invalid":   {{Input: 0, Stream: "", Output: 0}},
		"duplicate": {{Input: 0, Stream: "audio", Output: 0}, {Input: 0, Stream: "audio", Output: 0}},
	} {
		t.Run(name, func(t *testing.T) {
			description := testDescription(t)
			description.Mappings = mappings
			description.Boundaries = mappingBoundaries()
			_, err := New(description)
			if err == nil {
				t.Fatal("invalid mapping projection was accepted")
			}
			if name == "duplicate" && !strings.Contains(err.Error(), "duplicate mappings") {
				t.Fatalf("duplicate mapping error = %v", err)
			}
		})
	}
}

func TestPlanFingerprintExcludesDisplayAndIncludesExecutionState(t *testing.T) {
	base := testDescription(t)
	first, err := New(base)
	if err != nil {
		t.Fatal(err)
	}
	display := testDescription(t)
	display.Nodes[0].DisplayName = "Renamed"
	display.Warnings = []string{"different wording"}
	second, err := New(display)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint() != second.Fingerprint() || first.ExecutionSignature() != second.ExecutionSignature() {
		t.Fatal("display-only data changed canonical identity")
	}

	changed := testDescription(t)
	changed.Nodes[0].Outputs[0].Descriptor.TimeBaseDenominator = 44100
	third, err := New(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint() == third.Fingerprint() || first.ExecutionSignature() == third.ExecutionSignature() {
		t.Fatal("execution descriptor did not change canonical identity")
	}
}

func TestPlanFingerprintExcludesConfigProvenance(t *testing.T) {
	configuration := planConfiguration()
	explicit, err := configuration.View().Resolve(config.NewPatch().Set(planKey(t, "mode"), "copy"))
	if err != nil {
		t.Fatal(err)
	}
	planned, err := configuration.View().Resolve(config.NewPatch().Set(planKey(t, "mode"), "copy").Planned())
	if err != nil {
		t.Fatal(err)
	}
	if explicit.Fingerprint() != planned.Fingerprint() || explicit.Summary().Fields()[0].Source == planned.Summary().Fields()[0].Source {
		t.Fatalf("config provenance fixture = explicit %#v, planned %#v", explicit.Summary().Fields(), planned.Summary().Fields())
	}
	firstDescription := testDescription(t)
	firstDescription.Nodes[0].Config = explicit.Summary()
	first, err := New(firstDescription)
	if err != nil {
		t.Fatal(err)
	}
	secondDescription := testDescription(t)
	secondDescription.Nodes[0].Config = planned.Summary()
	second, err := New(secondDescription)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint() != second.Fingerprint() || first.ExecutionSignature() != second.ExecutionSignature() {
		t.Fatal("config provenance changed Plan identity")
	}
}

func TestPlanRuntimeProjectionIsCanonicalImmutableAndExecutable(t *testing.T) {
	description := testDescription(t)
	description.Runtime = Runtime{
		Executable: true,
		Islands: []Island{
			{ID: "island-1", Nodes: []string{"sink"}},
			{ID: "island-0", Nodes: []string{"source"}},
		},
		Buffers: []Buffer{{
			ID:       "source:out->sink:in",
			FromNode: "source",
			FromPort: "out",
			ToNode:   "sink",
			ToPort:   "in",
			Limit:    Limit{Items: 4},
			Reason:   SourceBuffer | SinkBuffer,
		}},
	}
	planned, err := New(description)
	if err != nil {
		t.Fatal(err)
	}
	runtime := planned.Runtime()
	if !runtime.Executable || runtime.Islands[0].ID != "island-0" {
		t.Fatalf("runtime = %#v", runtime)
	}
	runtime.Islands[0].Nodes[0] = "changed"
	if planned.Runtime().Islands[0].Nodes[0] != "source" {
		t.Fatal("Plan exposed mutable runtime projection")
	}
	changed := testDescription(t)
	changed.Runtime = description.Runtime
	changed.Runtime.Buffers[0].Limit.Items = 8
	other, err := New(changed)
	if err != nil {
		t.Fatal(err)
	}
	if planned.ExecutionSignature() == other.ExecutionSignature() {
		t.Fatal("runtime buffer change did not affect execution signature")
	}
}

func TestPlanRuntimeFanInAllowsOneLogicalRepeatedEdge(t *testing.T) {
	description := testDescription(t)
	description.Runtime = Runtime{
		Executable: true,
		Islands: []Island{
			{ID: "island-0", Nodes: []string{"source"}},
			{ID: "island-1", Nodes: []string{"sink"}},
		},
		FanIns: []FanIn{{Node: "sink", Port: "in", Policy: flow.ZipFanIn, Tolerance: 250 * time.Millisecond}},
	}
	if _, err := New(description); err != nil {
		t.Fatalf("one logical repeated fan-in edge rejected: %v", err)
	}
}

func planKey(t *testing.T, field string) config.Key {
	t.Helper()
	key, ok := planConfiguration().Key(field)
	if !ok {
		t.Fatalf("plan fixture schema has no %s field", field)
	}
	return key
}
