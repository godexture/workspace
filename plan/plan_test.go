package plan

import (
	"fmt"
	"strings"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type planConfigID struct{}
type planSchemaID struct{}
type planUnit struct{}

type planConfig struct {
	Mode  string
	Token config.SecretValue[string]
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
	boundaries[0].Available[0] = access.CancelableRead
	if first.Boundaries()[0].Available[0] != access.RandomRead {
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

func testDescription(t *testing.T) Description {
	t.Helper()
	configuration := config.Struct[planConfigID](func() planConfig {
		return planConfig{Mode: "copy", Token: config.NewSecret("default-secret")}
	}).
		Version("1").
		AddField(config.Field("mode", func(value *planConfig) *string { return &value.Mode }, config.String())).
		AddField(config.Field("token", func(value *planConfig) *config.SecretValue[string] { return &value.Token }, config.SecretCodec(config.String()))).
		Build()
	resolved, err := configuration.View().Resolve(config.NewPatch().SetText("token", "live-secret"))
	if err != nil {
		t.Fatal(err)
	}
	typ := schema.Define[planSchemaID, planUnit](schema.Traits[planUnit]{})
	descriptor, err := ProjectDescriptor(stream.MustDescriptor("audio", typ.Identity(), timing.MustBase(1, 48000), property.New()))
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
		Warnings: []string{"display warning"},
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
