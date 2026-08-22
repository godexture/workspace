package format

import (
	"context"
	"slices"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

type fixtureFormatID struct{}
type fixturePacketizedFormatID struct{}
type fixtureOtherFormatID struct{}
type fixtureCarrierID struct{}
type fixtureReadComponentID struct{}
type fixtureWriteComponentID struct{}
type fixtureConfigID struct{}
type fixtureUnit int
type frozenInspection struct{ Values []int }

type fixtureInspectSession struct{ capabilities access.Capabilities }

func (s fixtureInspectSession) Capabilities() access.Capabilities { return s.capabilities }
func (fixtureInspectSession) Close() error                        { return nil }
func (fixtureInspectSession) ReadAt(context.Context, []byte, int64) (int, error) {
	return 0, nil
}

func TestFormatDeclaresIdentityAndOpenCarriers(t *testing.T) {
	declared := carrier.Define[fixtureCarrierID]()
	value, err := Define[fixtureFormatID]([]carrier.ID{declared}, WithExtensions(".WAVE", "wav"))
	if err != nil {
		t.Fatal(err)
	}
	if !value.Valid() || value.Carriers()[0] != declared || value.Packetized() || !slices.Equal(value.Extensions(), []Extension{"wav", "wave"}) {
		t.Fatalf("format = %#v", value)
	}
	exts := value.Extensions()
	exts[0] = "changed"
	if value.Extensions()[0] != "wav" {
		t.Fatal("Format exposed mutable extensions")
	}
	if _, err := Define[struct{}](nil); err == nil {
		t.Fatal("empty format identity accepted")
	}
	for _, values := range [][]string{{"wav", ".WAV"}, {""}, {"a/b"}, {"two.parts"}} {
		if _, err := Define[fixtureOtherFormatID](nil, WithExtensions(values...)); err == nil {
			t.Fatalf("invalid extensions accepted: %v", values)
		}
	}
}

func TestFormatDeclarationEqualityIncludesExtensions(t *testing.T) {
	left, _ := Define[fixtureFormatID](nil, WithExtensions("wave", "wav"))
	right, _ := Define[fixtureFormatID](nil, WithExtensions(".WAV", ".WAVE"))
	other, _ := Define[fixtureFormatID](nil, WithExtensions("wave"))
	if !left.Same(right) || left.Same(other) {
		t.Fatalf("Format Same = %v/%v", left.Same(right), left.Same(other))
	}
}

func TestPacketizedFormatMayOmitCapabilityAlternative(t *testing.T) {
	value, err := DefinePacketized[fixturePacketizedFormatID](nil)
	if err != nil || !value.Packetized() {
		t.Fatalf("packetized format = %#v, %v", value, err)
	}
}

func TestDirectionTraitsOwnIndependentCapabilityAlternatives(t *testing.T) {
	value, err := Define[fixtureFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	schema := config.Struct[fixtureConfigID](func() struct{} { return struct{}{} }).Version("1").Build()
	readComponent := plugin.NewComponent[fixtureReadComponentID](plugin.Descriptor{DisplayName: "read"}, schema,
		Read(value, access.NewRequirements(access.AllOf(access.SequentialRead), access.AllOf(access.RandomRead, access.StableSize))))
	writeComponent := plugin.NewComponent[fixtureWriteComponentID](plugin.Descriptor{DisplayName: "write"}, schema,
		Write(value, access.NewRequirements(access.AllOf(access.SequentialWrite))))
	read, readOK := ReadOf(readComponent)
	write, writeOK := WriteOf(writeComponent)
	if !readOK || !writeOK || !read.Valid() || !write.Valid() || read.Format().Identity() != value.Identity() || write.Format().Identity() != value.Identity() {
		t.Fatalf("traits = read %#v/%v, write %#v/%v", read, readOK, write, writeOK)
	}
	readRequirements := read.Requirements()
	writeRequirements := write.Requirements()
	if len(readRequirements.Alternatives) != 2 || len(writeRequirements.Alternatives) != 1 || writeRequirements.Alternatives[0].Capabilities[0] != access.SequentialWrite {
		t.Fatalf("requirements = read %#v, write %#v", readRequirements, writeRequirements)
	}
	readRequirements.Alternatives[0].Capabilities[0] = access.RandomWrite
	if read.Requirements().Alternatives[0].Capabilities[0] != access.SequentialRead {
		t.Fatal("ReadTrait leaked mutable requirements")
	}
}

func TestReadTraitTransportsTypedInspectionThroughCompileContext(t *testing.T) {
	value, err := Define[fixtureFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := access.NewCapabilities(access.RandomRead)
	if err != nil {
		t.Fatal(err)
	}
	selection, ok := access.Select(capabilities, access.NewRequirements(access.AllOf(access.RandomRead)))
	if !ok {
		t.Fatal("random read selection failed")
	}
	opening, err := access.NewOpening(access.SourceDirection, fixtureInspectSession{capabilities: capabilities}, selection, 0)
	if err != nil {
		t.Fatal(err)
	}
	type prepared struct{ Size int }
	called := 0
	schema := config.Struct[fixtureConfigID](func() struct{} { return struct{}{} }).Version("1").Build()
	component := plugin.NewComponent[fixtureReadComponentID](plugin.Descriptor{DisplayName: "inspected read"}, schema,
		Read(value, access.NewRequirements(access.AllOf(access.RandomRead)), WithInspect(func(ctx InspectContext) (Inspection, error) {
			called++
			if !ctx.Valid() || ctx.Context() == nil || !ctx.Opening().Valid() {
				t.Fatal("InspectContext is invalid")
			}
			return NewInspection(value, prepared{Size: 44}), nil
		})))
	trait, ok := ReadOf(component)
	if !ok || !trait.HasInspect() {
		t.Fatalf("read trait = %#v/%v", trait, ok)
	}
	inspection, err := trait.Inspect(NewInspectContext(context.Background(), opening, plugin.CompileContext{}, 1<<20, 2<<20))
	if err != nil || called != 1 {
		t.Fatalf("Inspect = %#v, %v; calls = %d", inspection, err, called)
	}
	compileContext, err := WithInspection(plugin.CompileContext{}, inspection)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := InspectionOf[prepared](compileContext, value)
	if !ok || got.Size != 44 {
		t.Fatalf("inspection = %#v/%v", got, ok)
	}
	other, err := Define[fixtureOtherFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := InspectionOf[prepared](compileContext, other); ok {
		t.Fatal("inspection accepted a different Format")
	}
}

func TestSelectionIsFormatBoundImmutableAndCanonical(t *testing.T) {
	value, err := Define[fixtureFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Define[fixtureOtherFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := []stream.ID{"video", "audio"}
	selection, err := NewSelection(value, ids...)
	if err != nil || !selection.Valid() {
		t.Fatalf("selection = %#v, %v", selection, err)
	}
	ids[0] = "changed"
	if got := selection.Streams(); !slices.Equal(got, []stream.ID{"audio", "video"}) {
		t.Fatalf("canonical streams = %#v", got)
	}
	streams := selection.Streams()
	streams[0] = "changed"
	if selection.Streams()[0] != "audio" {
		t.Fatal("Selection exposed mutable stream storage")
	}
	for name, ids := range map[string][]stream.ID{
		"empty":     nil,
		"zero":      {"audio", ""},
		"duplicate": {"audio", "audio"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewSelection(value, ids...); err == nil {
				t.Fatal("invalid selection was accepted")
			}
		})
	}
	if _, err := NewSelection(Format{}, stream.ID("audio")); err == nil {
		t.Fatal("selection for invalid Format was accepted")
	}
	context, err := WithSelection(plugin.CompileContext{}, selection)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := SelectionOf(context, value)
	if !ok || !slices.Equal(got.Streams(), []stream.ID{"audio", "video"}) || got.Format().Identity() != value.Identity() {
		t.Fatalf("selection context = %#v/%v", got, ok)
	}
	if _, ok := SelectionOf(context, other); ok {
		t.Fatal("selection accepted a different Format")
	}
	if _, ok := SelectionOf(plugin.CompileContext{}, value); ok {
		t.Fatal("missing selection did not retain preserve-all distinction")
	}
}

func TestInspectContextSeparatesReadAndRetainedMemoryLimits(t *testing.T) {
	_, err := Define[fixtureFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := access.NewCapabilities(access.RandomRead)
	if err != nil {
		t.Fatal(err)
	}
	selection, ok := access.Select(capabilities, access.NewRequirements(access.AllOf(access.RandomRead)))
	if !ok {
		t.Fatal("random read selection failed")
	}
	opening, err := access.NewOpening(access.SourceDirection, fixtureInspectSession{capabilities: capabilities}, selection, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx := NewInspectContext(context.Background(), opening, plugin.CompileContext{}, 17, 53)
	if !ctx.Valid() {
		t.Fatal("InspectContext is invalid")
	}
	if ctx.Limit() != 17 || ctx.MemoryLimit() != 53 {
		t.Fatalf("InspectContext limits = read %d, memory %d", ctx.Limit(), ctx.MemoryLimit())
	}
	if NewInspectContext(context.Background(), opening, plugin.CompileContext{}, 17, 0).Valid() {
		t.Fatal("zero retained-memory limit accepted")
	}
}

func TestInspectionSharesFrozenReferenceValue(t *testing.T) {
	value, err := Define[fixtureFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared := frozenInspection{Values: []int{1}}
	inspection := NewInspection(value, prepared)
	if !inspection.Valid() {
		t.Fatal("frozen reference-valued inspection is invalid")
	}
	ctx, err := WithInspection(plugin.CompileContext{}, inspection)
	if err != nil {
		t.Fatal(err)
	}
	first, ok := InspectionOf[frozenInspection](ctx, value)
	if !ok || len(first.Values) != 1 {
		t.Fatalf("first inspection = %#v/%v", first, ok)
	}
	second, ok := InspectionOf[frozenInspection](ctx, value)
	if !ok || len(second.Values) != 1 {
		t.Fatalf("second inspection = %#v/%v", second, ok)
	}
	if &prepared.Values[0] != &first.Values[0] || &first.Values[0] != &second.Values[0] {
		t.Fatal("InspectionOf copied the frozen reference value")
	}
}

func TestNewInspectionRejectsInvalidFormatAndNilValues(t *testing.T) {
	value, err := Define[fixtureFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	if NewInspection(Format{}, 1).Valid() {
		t.Fatal("invalid Format produced a valid inspection")
	}
	if NewInspection[any](value, nil).Valid() {
		t.Fatal("nil produced a valid inspection")
	}
	var pointer *int
	var slice []int
	var values map[string]int
	var channel chan int
	var function func()
	for name, prepared := range map[string]any{
		"pointer":  pointer,
		"slice":    slice,
		"map":      values,
		"channel":  channel,
		"function": function,
	} {
		if inspection := NewInspection(value, prepared); inspection.Valid() {
			t.Fatalf("typed-nil %s produced a valid inspection", name)
		}
	}
}

func TestReadTraitRunsPureBoundedProbe(t *testing.T) {
	value, err := Define[fixtureFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	rangeRequest, err := access.NewRangeRequest(0, 12)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := NewEvidence("RIFF/WAVE signature")
	if err != nil {
		t.Fatal(err)
	}
	type contextKey struct{}
	parent := context.WithValue(context.Background(), contextKey{}, "hidden")
	calls := 0
	schema := config.Struct[fixtureConfigID](func() struct{} { return struct{}{} }).Version("1").Build()
	component := plugin.NewComponent[fixtureReadComponentID](plugin.Descriptor{DisplayName: "probed read"}, schema,
		Read(value, access.NewRequirements(access.AllOf(access.RandomRead)), WithProbe(func(ctx ProbeContext) (ProbeResult, error) {
			calls++
			if !ctx.Valid() || ctx.Context().Value(contextKey{}) != nil {
				t.Fatal("ProbeContext exposed an invalid context or value")
			}
			views := ctx.Views()
			if len(views) == 0 {
				return Need(rangeRequest), nil
			}
			if end, known := ctx.End(); !known || end != 12 {
				t.Fatalf("source end = %d/%v", end, known)
			}
			return Match(evidence), nil
		})))
	trait, ok := ReadOf(component)
	if !ok || !trait.Valid() || !trait.HasProbe() {
		t.Fatalf("probe trait = %#v/%v", trait, ok)
	}
	requested, err := trait.Probe(NewProbeContext(parent, nil))
	if err != nil || requested.Status() != ProbeNeedsData || len(requested.Needs()) != 1 {
		t.Fatalf("initial Probe = %#v, %v", requested, err)
	}
	view := access.NewProbeView([]byte("RIFF\x00\x00\x00\x00WAVE"))
	matched, err := trait.Probe(NewProbeContextAtEnd(parent, []access.ProbeView{view}, 12))
	if err != nil || matched.Status() != ProbeMatch || len(matched.Evidence()) != 1 || calls != 2 {
		t.Fatalf("terminal Probe = %#v, %v; calls = %d", matched, err, calls)
	}
	if !Fallback().Valid() || !Mismatch().Valid() || !Malformed("truncated header", evidence).Valid() {
		t.Fatal("terminal Probe result constructors are invalid")
	}
}

func TestReadTraitDeclaresImmutableFallbackConfig(t *testing.T) {
	value, _ := Define[fixtureFormatID](nil)
	schema := config.Struct[fixtureConfigID](func() struct{} { return struct{}{} }).Version("1").Build()
	component := plugin.NewComponent[fixtureReadComponentID](plugin.Descriptor{DisplayName: "fallback"}, schema,
		Read(value, access.NewRequirements(access.AllOf(access.SequentialRead)), WithProbe(func(ProbeContext) (ProbeResult, error) {
			return Fallback(), nil
		}), RequireFallbackConfig("rate", "layout")))
	trait, ok := ReadOf(component)
	if !ok || !trait.Valid() || !slices.Equal(trait.FallbackConfigFields(), []string{"layout", "rate"}) {
		t.Fatalf("fallback trait = %#v/%v", trait, ok)
	}
	fields := trait.FallbackConfigFields()
	fields[0] = "changed"
	if trait.FallbackConfigFields()[0] != "layout" {
		t.Fatal("ReadTrait exposed mutable fallback config")
	}
	invalid := plugin.NewComponent[fixtureWriteComponentID](plugin.Descriptor{DisplayName: "invalid fallback"}, schema,
		Read(value, access.NewRequirements(access.AllOf(access.SequentialRead)), RequireFallbackConfig("rate")))
	invalidTrait, _ := ReadOf(invalid)
	if invalidTrait.Valid() {
		t.Fatal("fallback config without Probe is valid")
	}
}
