package metadata

import (
	"bytes"
	"strings"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plugin"
)

type mappingBytesSourceID struct{}
type mappingBytesTargetID struct{}

var mappingBytesSource = key.Define[mappingBytesSourceID, []byte](func(value []byte) []byte { return append([]byte(nil), value...) })
var mappingBytesTarget = key.Define[mappingBytesTargetID, []byte](func(value []byte) []byte { return append([]byte(nil), value...) })

func moodToGenre(value string) (string, bool) {
	switch value {
	case "melancholic":
		return "Blues", true
	}
	return "", false
}

func TestMappingDeclaresDirectionLossinessAndPriority(t *testing.T) {
	mapping := Map(mood, genre, loss.Ambiguous, 10, moodToGenre)
	if !mapping.Valid() {
		t.Fatalf("mapping problem = %v", mapping.Problem())
	}
	if mapping.Source() != mood.ID() || mapping.Target() != genre.ID() {
		t.Fatalf("mapping direction = %s -> %s", mapping.Source(), mapping.Target())
	}
	if mapping.Lossiness() != loss.Ambiguous || mapping.Priority() != 10 {
		t.Fatalf("mapping = %v, %d", mapping.Lossiness(), mapping.Priority())
	}
	value, ok := mapping.Convert("melancholic")
	if !ok || value.(string) != "Blues" {
		t.Fatalf("conversion = %v, %v", value, ok)
	}
}

func TestMappingDeclinesRatherThanGuessing(t *testing.T) {
	mapping := Map(mood, genre, loss.Ambiguous, 0, moodToGenre)
	// A value the conversion does not recognise is declined, so the caller
	// reports a loss instead of inventing a target value.
	if _, ok := mapping.Convert("unmapped"); ok {
		t.Fatal("unrecognised value was converted")
	}
	// A value of the wrong type never reaches the typed conversion.
	if _, ok := mapping.Convert(42); ok {
		t.Fatal("value of the wrong type was converted")
	}
}

func TestMappingWithoutADeclaredContractIsRejected(t *testing.T) {
	if Map(mood, genre, loss.Ambiguous, 0, nil).Valid() {
		t.Fatal("mapping without a conversion accepted")
	}
	if Map(mood, genre, loss.Lossiness(0), 0, moodToGenre).Valid() {
		t.Fatal("mapping without declared lossiness accepted")
	}
	if Map(mood, mood, loss.Lossless, 0, func(value string) (string, bool) { return value, true }).Valid() {
		t.Fatal("mapping from a key to itself accepted")
	}
	var missing key.Key[string]
	if Map(missing, genre, loss.Lossless, 0, func(value string) (string, bool) { return value, true }).Valid() {
		t.Fatal("mapping with an invalid source key accepted")
	}
	if Map(mood, missing, loss.Lossless, 0, func(value string) (string, bool) { return value, true }).Valid() {
		t.Fatal("mapping with an invalid target key accepted")
	}
	problem := Map(mood, genre, loss.Lossiness(0), 0, moodToGenre).Problem()
	if problem == nil || !strings.Contains(problem.Error(), "lossiness") {
		t.Fatalf("problem = %v", problem)
	}
}

func TestMappingOrderIsTotalAndIndependentOfDeclarationOrder(t *testing.T) {
	high := Map(mood, genre, loss.Ambiguous, 10, moodToGenre)
	low := Map(mood, genre, loss.Lossless, 1, moodToGenre)
	if !high.Better(low) || low.Better(high) {
		t.Fatal("priority did not decide")
	}
	lossless := Map(artist, genre, loss.Lossless, 5, func(value string) (string, bool) { return value, true })
	ambiguous := Map(mood, genre, loss.Ambiguous, 5, moodToGenre)
	if !lossless.Better(ambiguous) || ambiguous.Better(lossless) {
		t.Fatal("lossiness did not break the priority tie")
	}
	first := Map(mood, genre, loss.Lossless, 5, func(value string) (string, bool) { return value, true })
	second := Map(mood, title, loss.Lossless, 5, func(value string) (string, bool) { return value, true })
	if first.Better(second) == second.Better(first) {
		t.Fatal("equally ranked mappings have no stable order")
	}
}

func TestMappingTraitValidatesAndDoesNotExposeItsStorage(t *testing.T) {
	mapping := Map(mood, genre, loss.Lossless, 3, func(value string) (string, bool) { return value, true })
	component := plugin.NewComponent[encodingTraitComponentID](plugin.Descriptor{DisplayName: "mapping"}, config.Struct[encodingTraitConfigID](func() struct{} { return struct{}{} }).Version("1").Build(), WithMappings(mapping))
	declared, ok := MappingsOf(component)
	if !ok || !declared.Valid() || len(declared.Values()) != 1 {
		t.Fatalf("MappingsOf = %#v/%v", declared, ok)
	}
	values := declared.Values()
	values[0].priority = 99
	if got := declared.Values()[0].Priority(); got != 3 {
		t.Fatalf("Mappings.Values exposed trait storage: priority = %d", got)
	}
	if traits := component.Traits(); len(traits) != 1 || !strings.Contains(traits[0].Manifest, mapping.Source().String()+">"+mapping.Target().String()) {
		t.Fatalf("mapping manifest = %#v", traits)
	}
	if value := newMappings(nil); value.Valid() {
		t.Fatal("empty mapping trait is valid")
	}
	if value := newMappings([]Mapping{mapping, mapping}); value.Valid() {
		t.Fatal("duplicate mapping trait is valid")
	}
}

func TestMappingSnapshotsReferenceSourceBeforeConversion(t *testing.T) {
	component, _ := projectionEncoding(mappingBytesTarget.Erased())
	mapping := Map(mappingBytesSource, mappingBytesTarget, loss.Lossless, 0, func(value []byte) ([]byte, bool) {
		value[0] = 9
		return value, true
	})
	resolver := projectionResolver(t, component, mapping)
	document := projectionDocument(t, func(builder *Builder) {
		Add(builder, mappingBytesSource, []byte{1, 2}, Origin{})
	})
	projected, _, err := resolver.Project(testCarrier, "target", document)
	if err != nil {
		t.Fatal(err)
	}
	if source := Values(document, mappingBytesSource); len(source) != 1 || !bytes.Equal(source[0], []byte{1, 2}) {
		t.Fatalf("source document was mutated by converter: %v", source)
	}
	if target := Values(projected, mappingBytesTarget); len(target) != 1 || !bytes.Equal(target[0], []byte{9, 2}) {
		t.Fatalf("projected target = %v", target)
	}
}

func TestBindingNamesTheEncodingThatInterpretsACarrier(t *testing.T) {
	binding := Bind(testCarrier, encodingIdentity())
	if !binding.Valid() {
		t.Fatalf("binding = %v", binding)
	}
	targets := binding.Targets()
	if len(targets) != 1 {
		t.Fatalf("binding targets = %v", targets)
	}
	target, componentTarget := targets[0].Component()
	if !componentTarget || target != encodingIdentity() {
		t.Fatalf("binding targets = %v", targets)
	}
	if binding.Key() != BindingKey(testCarrier) {
		t.Fatalf("binding key = %v, want %v", binding.Key(), BindingKey(testCarrier))
	}
	// A codec binding and a metadata binding that happen to share a key string
	// live in different namespaces, so they never collide.
	if binding.Key().Namespace() == plugin.Declare[struct{ other int }]("wave.id3").Key().Namespace() {
		t.Fatal("metadata bindings share a namespace with unrelated declarations")
	}
}

func TestConflictingBindingsForOneCarrierKeepTheirDistinctTargets(t *testing.T) {
	first := Bind(testCarrier, encodingIdentity())
	second := Bind(testCarrier, otherEncodingIdentity())
	if first.Key() != second.Key() {
		t.Fatal("same carrier produced different declaration keys")
	}
	// Detection belongs to host construction; the declaration only has to keep
	// the difference visible instead of applying last-wins.
	if first.SameTargets(second) {
		t.Fatal("conflicting bindings reported the same target")
	}
}
