package catalog

import (
	"context"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/endpoint"
	"github.com/godexture/godec/flow"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/plugin"
)

type catalogPluginID struct{}
type catalogFirstID struct{}
type catalogSecondID struct{}
type catalogUnitID struct{}
type catalogFormatID struct{}
type catalogOtherFormatID struct{}
type catalogControlTraitID struct{}
type catalogControlComponentID struct{}
type catalogRequiredControlComponentID struct{}
type catalogEmptyComponentID struct{}

type catalogConfig struct{ Value int }
type catalogUnit int
type catalogOtherUnit int

type catalogOperator struct{ shape flow.Shape }
type catalogSession struct{ capabilities access.Capabilities }

func (o catalogOperator) Ports() flow.Shape { return o.shape.Clone() }
func (catalogOperator) Close() error        { return nil }
func (s catalogSession) Capabilities() access.Capabilities {
	result, _ := access.NewCapabilities(s.capabilities.Values()...)
	return result
}
func (catalogSession) Close() error { return nil }

func catalogSchema() config.Schema[catalogConfig] {
	return config.Struct[catalogConfig](func() catalogConfig { return catalogConfig{Value: 1} }).
		Version("1").
		AddField(config.Field("value", func(value *catalogConfig) *int { return &value.Value }, config.Int().Range(0, 10))).
		Build()
}

func catalogComponent[Marker any](name string) plugin.Component {
	return catalogComponentWithSchema[Marker](plugin.Descriptor{DisplayName: name, Version: "1.0.0"}, catalogSchema())
}

func catalogComponentWithSchema[Marker any](descriptor plugin.Descriptor, schemaValue config.Schema[catalogConfig], contracts ...plugin.Contract) plugin.Component {
	typ := schema.Define[catalogUnitID, catalogUnit](schema.Traits[catalogUnit]{})
	shape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	spec := plugin.Spec[catalogConfig, flow.Shape, int]{
		Ports: shape,
		Compile: func(plugin.CompileContext, catalogConfig, flow.Descriptors[int]) (plugin.Compiled[flow.Shape, int], error) {
			return plugin.Compiled[flow.Shape, int]{Plan: shape, Outputs: flow.NewDescriptors(flow.Describe("out", 1))}, nil
		},
		Open: func(_ plugin.OpenContext, plan flow.Shape) (flow.Operator, error) {
			return catalogOperator{shape: plan}, nil
		},
	}
	if len(contracts) != 0 {
		spec.Contract = contracts[0]
	}
	return plugin.NewComponent[Marker](descriptor, schemaValue, plugin.WithSpec(spec))
}

func catalogTraitComponent[Marker any](name string, shape flow.Shape, options ...plugin.ComponentOption) plugin.Component {
	spec := plugin.Spec[catalogConfig, flow.Shape, int]{
		Ports: shape,
		Compile: func(plugin.CompileContext, catalogConfig, flow.Descriptors[int]) (plugin.Compiled[flow.Shape, int], error) {
			outputs := flow.NewDescriptors[int]()
			if len(shape.Outputs) == 1 {
				outputs = flow.NewDescriptors(flow.Describe(shape.Outputs[0].ID(), 1))
			}
			return plugin.Compiled[flow.Shape, int]{Plan: shape, Outputs: outputs}, nil
		},
		Open: func(_ plugin.OpenContext, plan flow.Shape) (flow.Operator, error) {
			return catalogOperator{shape: plan}, nil
		},
	}
	options = append([]plugin.ComponentOption{plugin.WithSpec(spec)}, options...)
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: name, Version: "1"}, catalogSchema(), options...)
}

func catalogAcquire(capabilities access.Capabilities) access.AcquireFunc {
	return func(context.Context, access.Reference, access.Selection) (access.Session, error) {
		return catalogSession{capabilities: capabilities}, nil
	}
}

func TestBuildValidatesAndSortsImmutableIndex(t *testing.T) {
	first := catalogComponent[catalogFirstID]("first")
	second := catalogComponent[catalogSecondID]("second")
	definition := plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "catalog", Version: "1.0.0"}, second, first)
	set := plugin.NewSet(definition)
	index, err := Build(set)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if index.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", index.Len())
	}
	components := index.Components()
	if components[0].Identity().String() >= components[1].Identity().String() {
		t.Fatalf("components are not sorted by identity")
	}
	views := index.Views()
	views[0].Aliases = append(views[0].Aliases, "mutated")
	if len(index.Views()[0].Aliases) != 0 {
		t.Fatalf("catalog view aliases are mutable")
	}
}

func TestBuildAcceptsPortlessTraitAndRejectsMissingComponentContract(t *testing.T) {
	key := plugin.TraitKeyOf[catalogControlTraitID]()
	control := plugin.NewComponent[catalogControlComponentID](
		plugin.Descriptor{DisplayName: "control", Version: "1"},
		catalogSchema(),
		plugin.WithTrait(key, "fixture=control", plugin.PortShapeOptional, true),
	)
	definition := plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "catalog", Version: "1"}, control)
	index, err := Build(plugin.NewSet(definition))
	if err != nil {
		t.Fatal(err)
	}
	view := index.Views()[0]
	if view.HasSpec || view.Executable || !view.Ports.Empty() || len(view.Traits) != 1 {
		t.Fatalf("control-plane catalog view = %#v", view)
	}

	required := plugin.NewComponent[catalogRequiredControlComponentID](
		plugin.Descriptor{DisplayName: "missing ports", Version: "1"},
		catalogSchema(),
		plugin.WithTrait(key, "fixture=required", plugin.PortShapeRequired, true),
	)
	definition = plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "catalog", Version: "1"}, required)
	if _, err := Build(plugin.NewSet(definition)); err == nil || !hasCatalogDiagnostic(err, "catalog.trait-shape") {
		t.Fatalf("required trait shape diagnostic = %v", err)
	}

	empty := plugin.NewComponent[catalogEmptyComponentID](plugin.Descriptor{DisplayName: "empty", Version: "1"}, catalogSchema())
	definition = plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "catalog", Version: "1"}, empty)
	if _, err := Build(plugin.NewSet(definition)); err == nil || !hasCatalogDiagnostic(err, "plugin.spec") {
		t.Fatalf("empty component diagnostic = %v", err)
	}
}

func TestImplementationContractChangesCatalogFingerprint(t *testing.T) {
	descriptor := plugin.Descriptor{DisplayName: "first", Version: "1.0.0"}
	build := func(contract ...plugin.Contract) Index {
		component := catalogComponentWithSchema[catalogFirstID](descriptor, catalogSchema(), contract...)
		definition := plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "catalog", Version: "1.0.0"}, component)
		index, err := Build(plugin.NewSet(definition))
		if err != nil {
			t.Fatal(err)
		}
		return index
	}
	base := build()
	stable := build(plugin.Contract{
		Accuracy:       plugin.ExactContract,
		Repeatability:  plugin.RepeatableContract,
		Artifact:       plugin.StableArtifactSupport,
		Implementation: plugin.PureGoImplementation,
	})
	if base.Fingerprint() == stable.Fingerprint() {
		t.Fatal("implementation contract did not affect catalog fingerprint")
	}
}

func TestTraitManifestChangesCatalogFingerprint(t *testing.T) {
	shape := flow.NewShape(nil, []flow.Port{flow.Out("out", access.Bytes())})
	build := func(capability access.Capability) Index {
		capabilities, _ := access.NewCapabilities(capability)
		component := catalogTraitComponent[catalogFirstID]("source", shape, access.Source("memory", capabilities, catalogAcquire(capabilities)))
		definition := plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "catalog", Version: "1"}, component)
		index, err := Build(plugin.NewSet(definition))
		if err != nil {
			t.Fatal(err)
		}
		return index
	}
	if build(access.SequentialRead).Fingerprint() == build(access.RandomRead).Fingerprint() {
		t.Fatal("Access trait change did not affect catalog fingerprint")
	}
}

func TestFormatExtensionsAreIndexedByDirectionAndAffectFingerprint(t *testing.T) {
	typ := schema.Define[catalogUnitID, catalogUnit](schema.Traits[catalogUnit]{})
	build := func(extension string) Index {
		value, err := mediaformat.Define[catalogFormatID](nil, mediaformat.WithExtensions(extension))
		if err != nil {
			t.Fatal(err)
		}
		read := catalogTraitComponent[catalogFirstID]("read", flow.NewShape(
			[]flow.Port{flow.In("bytes", access.Bytes())}, []flow.Port{flow.Out("out", typ)},
		), mediaformat.Read(value, access.NewRequirements(access.AllOf(access.SequentialRead))))
		write := catalogTraitComponent[catalogSecondID]("write", flow.NewShape(
			[]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("writes", access.Writes())},
		), mediaformat.Write(value, access.NewRequirements(access.AllOf(access.SequentialWrite))))
		index, err := Build(plugin.NewSet(plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "format", Version: "1"}, read, write)))
		if err != nil {
			t.Fatal(err)
		}
		return index
	}
	wav := build(".WAV")
	extension, _ := mediaformat.ParseExtension("wav")
	reads := wav.ReadExtension(extension)
	writes := wav.WriteExtension(extension)
	if len(reads) != 1 || reads[0].Component().Identity() != plugin.IdentityOf[catalogFirstID]() || len(writes) != 1 || writes[0].Component().Identity() != plugin.IdentityOf[catalogSecondID]() {
		t.Fatalf("directional extension index = read %#v, write %#v", reads, writes)
	}
	reads[0] = FormatMatch{}
	if got := wav.ReadExtension(extension); len(got) != 1 || !got[0].Valid() {
		t.Fatal("Format extension lookup exposed index storage")
	}
	if wav.Fingerprint() == build("wave").Fingerprint() {
		t.Fatal("Format extension did not affect catalog fingerprint")
	}
}

func TestBuildRejectsConflictingFormatDeclarationAndFallbackField(t *testing.T) {
	typ := schema.Define[catalogUnitID, catalogUnit](schema.Traits[catalogUnit]{})
	firstFormat, _ := mediaformat.Define[catalogFormatID](nil, mediaformat.WithExtensions("one"))
	secondFormat, _ := mediaformat.Define[catalogFormatID](nil, mediaformat.WithExtensions("two"))
	readShape := flow.NewShape([]flow.Port{flow.In("bytes", access.Bytes())}, []flow.Port{flow.Out("out", typ)})
	first := catalogTraitComponent[catalogFirstID]("first", readShape,
		mediaformat.Read(firstFormat, access.NewRequirements(access.AllOf(access.SequentialRead))))
	second := catalogTraitComponent[catalogSecondID]("second", readShape,
		mediaformat.Read(secondFormat, access.NewRequirements(access.AllOf(access.SequentialRead))))
	definition := plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "format", Version: "1"}, first, second)
	if _, err := Build(plugin.NewSet(definition)); err == nil || !hasCatalogDiagnostic(err, "catalog.format-declaration") {
		t.Fatalf("conflicting Format declaration diagnostic = %v", err)
	}

	fallback := catalogTraitComponent[catalogFirstID]("fallback", readShape,
		mediaformat.Read(firstFormat, access.NewRequirements(access.AllOf(access.SequentialRead)),
			mediaformat.WithProbe(func(mediaformat.ProbeContext) (mediaformat.ProbeResult, error) { return mediaformat.Fallback(), nil }),
			mediaformat.RequireFallbackConfig("absent")))
	definition = plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "format", Version: "1"}, fallback)
	if _, err := Build(plugin.NewSet(definition)); err == nil || !hasCatalogDiagnostic(err, "catalog.format-config") {
		t.Fatalf("fallback config field diagnostic = %v", err)
	}
}

func TestCatalogAllowsSharedExtensionUntilSelection(t *testing.T) {
	typ := schema.Define[catalogUnitID, catalogUnit](schema.Traits[catalogUnit]{})
	firstFormat, _ := mediaformat.Define[catalogFormatID](nil, mediaformat.WithExtensions("shared"))
	secondFormat, _ := mediaformat.Define[catalogOtherFormatID](nil, mediaformat.WithExtensions("shared"))
	shape := flow.NewShape([]flow.Port{flow.In("bytes", access.Bytes())}, []flow.Port{flow.Out("out", typ)})
	first := catalogTraitComponent[catalogFirstID]("first", shape, mediaformat.Read(firstFormat, access.NewRequirements(access.AllOf(access.SequentialRead))))
	second := catalogTraitComponent[catalogSecondID]("second", shape, mediaformat.Read(secondFormat, access.NewRequirements(access.AllOf(access.SequentialRead))))
	index, err := Build(plugin.NewSet(plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "format", Version: "1"}, first, second)))
	if err != nil {
		t.Fatal(err)
	}
	extension, _ := mediaformat.ParseExtension("shared")
	if got := index.ReadExtension(extension); len(got) != 2 {
		t.Fatalf("shared extension matches = %d, want 2", len(got))
	}
}

func TestBuildAcceptsSourceAndSinkTraitsForTheSameScheme(t *testing.T) {
	sourceCapabilities, _ := access.NewCapabilities(access.SequentialRead)
	sinkCapabilities, _ := access.NewCapabilities(access.SequentialWrite)
	source := catalogTraitComponent[catalogFirstID](
		"source",
		flow.NewShape(nil, []flow.Port{flow.Out("out", access.Bytes())}),
		access.Source("memory", sourceCapabilities, catalogAcquire(sourceCapabilities)),
	)
	sink := catalogTraitComponent[catalogSecondID](
		"sink",
		flow.NewShape([]flow.Port{flow.In("in", access.Writes())}, nil),
		access.Sink("memory", sinkCapabilities, access.AtomicReplace, catalogAcquire(sinkCapabilities)),
	)
	definition := plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "catalog", Version: "1"}, source, sink)
	if _, err := Build(plugin.NewSet(definition)); err != nil {
		t.Fatalf("source/sink composition = %v", err)
	}
}

func TestBuildRejectsTraitShapeMismatchAndDirectionalSchemeConflict(t *testing.T) {
	typ := schema.Define[catalogUnitID, catalogUnit](schema.Traits[catalogUnit]{})
	capabilities, _ := access.NewCapabilities(access.SequentialRead)
	acquire := catalogAcquire(capabilities)
	formatValue, _ := mediaformat.Define[catalogFormatID](nil)
	tests := map[string]struct {
		components []plugin.Component
		code       string
	}{
		"source shape": {
			components: []plugin.Component{catalogTraitComponent[catalogFirstID]("source", flow.NewShape([]flow.Port{flow.In("in", typ)}, nil), access.Source("memory", capabilities, acquire))},
			code:       "catalog.access-shape",
		},
		"source schema": {
			components: []plugin.Component{catalogTraitComponent[catalogFirstID]("source", flow.NewShape(nil, []flow.Port{flow.Out("out", typ)}), access.Source("memory", capabilities, acquire))},
			code:       "catalog.access-schema",
		},
		"source scheme": {
			components: []plugin.Component{
				catalogTraitComponent[catalogFirstID]("first", flow.NewShape(nil, []flow.Port{flow.Out("out", access.Bytes())}), access.Source("memory", capabilities, acquire)),
				catalogTraitComponent[catalogSecondID]("second", flow.NewShape(nil, []flow.Port{flow.Out("out", access.Bytes())}), access.Source("MEMORY", capabilities, acquire)),
			},
			code: "catalog.access-scheme",
		},
		"format read shape": {
			components: []plugin.Component{catalogTraitComponent[catalogFirstID]("read", flow.NewShape(nil, []flow.Port{flow.Out("out", access.Bytes())}), mediaformat.Read(formatValue, access.NewRequirements(access.AllOf(access.SequentialRead))))},
			code:       "catalog.format-shape",
		},
		"format read schema": {
			components: []plugin.Component{catalogTraitComponent[catalogFirstID]("read", flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)}), mediaformat.Read(formatValue, access.NewRequirements(access.AllOf(access.SequentialRead))))},
			code:       "catalog.format-schema",
		},
		"format write shape": {
			components: []plugin.Component{catalogTraitComponent[catalogFirstID]("write", flow.NewShape([]flow.Port{flow.In("in", typ)}, nil), mediaformat.Write(formatValue, access.NewRequirements(access.AllOf(access.SequentialWrite))))},
			code:       "catalog.format-shape",
		},
		"format write schema": {
			components: []plugin.Component{catalogTraitComponent[catalogFirstID]("write", flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)}), mediaformat.Write(formatValue, access.NewRequirements(access.AllOf(access.SequentialWrite))))},
			code:       "catalog.format-schema",
		},
		"format duplicate": {
			components: []plugin.Component{catalogTraitComponent[catalogFirstID]("read", flow.NewShape([]flow.Port{flow.In("in", access.Bytes())}, []flow.Port{flow.Out("out", typ)}),
				mediaformat.Read(formatValue, access.NewRequirements(access.AllOf(access.SequentialRead))),
				mediaformat.Read(formatValue, access.NewRequirements(access.AllOf(access.RandomRead))))},
			code: "plugin.trait-duplicate",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			definition := plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "catalog", Version: "1"}, test.components...)
			_, err := Build(plugin.NewSet(definition))
			if err == nil || !hasCatalogDiagnostic(err, test.code) {
				t.Fatalf("diagnostic %s = %v", test.code, err)
			}
		})
	}
}

func TestFormatReadTraitAllowsDirectRoutedShapeOnly(t *testing.T) {
	typ := schema.Define[catalogUnitID, catalogUnit](schema.Traits[catalogUnit]{})
	formatValue, err := mediaformat.Define[catalogFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	requirements := access.NewRequirements(access.AllOf(access.SequentialRead))
	tests := map[string]struct {
		shape flow.Shape
		valid bool
	}{
		"carrier": {
			shape: flow.NewShape([]flow.Port{flow.In("bytes", access.Bytes())}, []flow.Port{flow.Out("packets", typ)}),
			valid: true,
		},
		"direct routed": {
			shape: flow.NewShape(nil, []flow.Port{flow.Out("packets", typ, flow.Many())}),
			valid: true,
		},
		"hybrid outputs": {
			shape: flow.NewShape(nil, []flow.Port{flow.Out("packets", typ, flow.Many()), flow.Out("side", typ)}),
		},
		"zero outputs": {
			shape: flow.NewShape(nil, nil),
		},
		"one output": {
			shape: flow.NewShape(nil, []flow.Port{flow.Out("packets", typ)}),
		},
		"multiple routed outputs": {
			shape: flow.NewShape(nil, []flow.Port{flow.Out("packets", typ, flow.Many()), flow.Out("side", typ, flow.Many())}),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			component := catalogTraitComponent[catalogFirstID]("read", test.shape, mediaformat.Read(formatValue, requirements))
			_, err := Build(plugin.NewSet(plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "catalog", Version: "1"}, component)))
			if test.valid {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !hasCatalogDiagnostic(err, "catalog.format-shape") {
				t.Fatalf("direct read shape diagnostic = %v", err)
			}
		})
	}
}

func TestBuildRejectsEndpointTraitOnNondirectionalComponent(t *testing.T) {
	typ := schema.Define[catalogUnitID, catalogUnit](schema.Traits[catalogUnit]{})
	trait, _ := endpoint.NewTrait(endpoint.LiveStatic, endpoint.Realtime)
	component := catalogTraitComponent[catalogFirstID](
		"endpoint",
		flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)}),
		endpoint.WithTrait(trait),
	)
	definition := plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "catalog", Version: "1"}, component)
	_, err := Build(plugin.NewSet(definition))
	if err == nil || !hasCatalogDiagnostic(err, "catalog.endpoint-shape") {
		t.Fatalf("endpoint shape diagnostic = %v", err)
	}
}

func hasCatalogDiagnostic(err error, code string) bool {
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == code {
			return true
		}
	}
	return false
}

func TestBuildRejectsBrokenDefinitionWithoutDroppingErrors(t *testing.T) {
	badSchema := config.Struct[catalogConfig](func() catalogConfig { return catalogConfig{} }).
		AddField(config.Field("value", func(value *catalogConfig) *int { return &value.Value }, config.Int(), config.DependsOn("unknown"))).
		AddField(config.Field("value", func(value *catalogConfig) *int { return &value.Value }, config.Int())).
		Build()
	bad := plugin.Define[catalogPluginID](plugin.Descriptor{}, catalogComponentWithSchema[catalogFirstID](plugin.Descriptor{}, badSchema))
	set := plugin.NewSet(bad)
	_, err := Build(set)
	if err == nil {
		t.Fatal("broken definition was accepted")
	}
	items := diagnostic.ItemsOf(err)
	if len(items) < 5 {
		t.Fatalf("got %d diagnostics, want aggregate: %v", len(items), err)
	}
}

func TestBuildIncludesSetCompositionDiagnostics(t *testing.T) {
	first := plugin.Define[catalogPluginID](
		plugin.Descriptor{DisplayName: "catalog", Version: "1.0.0"},
		catalogComponent[catalogFirstID]("first"),
	)
	duplicate := plugin.Define[catalogPluginID](
		plugin.Descriptor{DisplayName: "catalog replacement", Version: "1.0.0"},
		catalogComponent[catalogSecondID]("second"),
	)
	set := plugin.NewSet(first).Add(duplicate)

	_, err := Build(set)
	if err == nil {
		t.Fatal("Build accepted retained set composition diagnostics")
	}
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == "plugin.duplicate-identity" {
			return
		}
	}
	t.Fatalf("set composition diagnostic was not aggregated: %v", err)
}

func TestCatalogComponentResolvesPatch(t *testing.T) {
	component := catalogComponent[catalogFirstID]("first")
	definition := plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "catalog", Version: "1.0.0"}, component)
	index, err := Build(plugin.NewSet(definition))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	fromCatalog, ok := index.Lookup(component.Identity())
	if !ok {
		t.Fatal("catalog component lookup failed")
	}
	resolved, err := fromCatalog.Resolve(config.NewPatch().SetText("value", "7"))
	if err != nil {
		t.Fatalf("catalog component resolve failed: %v", err)
	}
	snapshot, err := resolved.Value()
	if err != nil {
		t.Fatalf("resolved snapshot failed: %v", err)
	}
	value, ok := snapshot.(catalogConfig)
	if !ok || value.Value != 7 {
		t.Fatalf("resolved catalog value = %#v, want catalogConfig{Value: 7}", snapshot)
	}

	_, err = fromCatalog.Resolve(config.NewPatch().SetText("value", "99").SetText("unknown", "1"))
	if err == nil {
		t.Fatal("invalid catalog patch unexpectedly resolved")
	}
	paths := make(map[string]bool)
	for _, item := range diagnostic.ItemsOf(err) {
		paths[item.Path.String()] = true
	}
	if !paths["value"] || !paths["unknown"] {
		t.Fatalf("catalog resolver diagnostics lack field paths: %v", err)
	}
}

func TestBuildRejectsMarkerSharedByPluginAndComponent(t *testing.T) {
	definition := plugin.Define[catalogPluginID](
		plugin.Descriptor{DisplayName: "catalog", Version: "1.0.0"},
		catalogComponent[catalogPluginID]("shared"),
	)
	_, err := Build(plugin.NewSet(definition))
	if err == nil {
		t.Fatal("a marker used by both a plugin and a component was accepted")
	}
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == "catalog.identity-conflict" {
			return
		}
	}
	t.Fatalf("identity conflict diagnostic missing: %v", err)
}

func TestBuildRejectsSchemaMarkerBoundToDifferentPayloadTypes(t *testing.T) {
	typ := schema.Define[catalogUnitID, catalogOtherUnit](schema.Traits[catalogOtherUnit]{})
	shape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	spec := plugin.Spec[catalogConfig, flow.Shape, int]{
		Ports: shape,
		Compile: func(plugin.CompileContext, catalogConfig, flow.Descriptors[int]) (plugin.Compiled[flow.Shape, int], error) {
			return plugin.Compiled[flow.Shape, int]{Plan: shape, Outputs: flow.NewDescriptors(flow.Describe("out", 1))}, nil
		},
		Open: func(_ plugin.OpenContext, plan flow.Shape) (flow.Operator, error) {
			return catalogOperator{shape: plan}, nil
		},
	}
	conflicting := plugin.NewComponent[catalogSecondID](plugin.Descriptor{DisplayName: "conflicting", Version: "1"}, catalogSchema(), plugin.WithSpec(spec))
	definition := plugin.Define[catalogPluginID](
		plugin.Descriptor{DisplayName: "catalog", Version: "1"},
		catalogComponent[catalogFirstID]("first"),
		conflicting,
	)
	_, err := Build(plugin.NewSet(definition))
	if err == nil {
		t.Fatal("schema payload conflict was accepted")
	}
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == "catalog.schema-conflict" {
			return
		}
	}
	t.Fatalf("schema conflict diagnostic missing: %v", err)
}
