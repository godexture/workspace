package integration_test

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/integration/acme"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/file"
	"github.com/godexture/godec/standard"
)

func TestStandardSetAddRunsThirdPartyProviderFormatCodecAndMetadata(t *testing.T) {
	label := "extension"
	payload := []byte{0, 4, 41, 254}
	encoded := mustACME(t, label, payload)
	inputReference, err := acme.Reference(encoded)
	if err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(t.TempDir(), "result.acme")
	outputReference := localFileReference(t, outputPath)
	input, err := job.InputFromReference(inputReference)
	if err != nil {
		t.Fatal(err)
	}
	output, err := job.OutputToReference(outputReference)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := job.NewGraph(
		[]job.Node{job.NewNode("writer", acme.WriterIdentity(), config.NewPatch())},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New([]job.Input{input}, []job.Output{output}, graph)
	if err != nil {
		t.Fatal(err)
	}
	definition := acme.Plugin()
	instance, err := host.New(host.Plugins(standard.Set().Add(definition)))
	if err != nil {
		t.Fatal(err)
	}
	assertACMEDeclarations(t, instance.Catalog().Declarations(), definition.Identity())
	prepared, err := instance.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.Close(); err != nil {
			t.Error(err)
		}
	}()
	assertACMEPlan(t, prepared.Plan())
	result, err := prepared.Run(t.Context())
	if err != nil || !result.Succeeded() {
		t.Fatalf("third-party Run = %#v, %v", result, err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want := mustACME(t, label, []byte{1, 5, 42, 255})
	if !bytes.Equal(got, want) {
		t.Fatalf("third-party output = %x, want %x", got, want)
	}
}

func TestThirdPartyFormatExtensionJoinsFileConvenienceThroughSetAdd(t *testing.T) {
	label := "file-extension"
	payload := []byte{0, 4, 41, 254}
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.acme")
	outputPath := filepath.Join(directory, "output.acme")
	if err := os.WriteFile(inputPath, mustACME(t, label, payload), 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := standard.NewFileJob(inputPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := host.New(host.Plugins(standard.Set().Add(acme.Plugin())))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := instance.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.Close(); err != nil {
			t.Error(err)
		}
	}()
	selectedReader, selectedWriter := false, false
	for _, node := range prepared.Plan().Nodes() {
		switch node.Component {
		case acme.ReaderIdentity().String():
			selectedReader = node.Origin == plan.Automatic && node.Reason == "format.probe"
		case acme.WriterIdentity().String():
			selectedWriter = node.Origin == plan.Automatic && node.Reason == "format.output"
		}
	}
	if !selectedReader || !selectedWriter {
		t.Fatalf("third-party Format selection = reader %v, writer %v", selectedReader, selectedWriter)
	}
	result, runErr := prepared.Run(t.Context())
	if runErr != nil || !result.Succeeded() {
		t.Fatalf("third-party file Run = %#v, %v", result, runErr)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want := mustACME(t, label, []byte{1, 5, 42, 255})
	if !bytes.Equal(got, want) {
		t.Fatalf("third-party file output = %x, want %x", got, want)
	}
}

func assertACMEDeclarations(t testing.TB, declarations []plugin.Declaration, owner plugin.Identity) {
	t.Helper()
	want := map[plugin.DeclarationKey]plugin.Identity{
		codec.BindingKey(acme.CodecTag()):        acme.DecoderIdentity(),
		metadata.BindingKey(acme.LabelCarrier()): acme.EncodingIdentity(),
	}
	for _, declaration := range declarations {
		target, wanted := want[declaration.Key()]
		if !wanted || declaration.Owner() != owner {
			continue
		}
		targets := declaration.Targets()
		if len(targets) != 1 {
			t.Errorf("owned declaration %s targets = %v, want %s", declaration.Key(), targets, target)
			delete(want, declaration.Key())
			continue
		}
		component, ok := targets[0].Component()
		if !ok || component != target {
			t.Errorf("owned declaration %s targets = %v, want %s", declaration.Key(), targets, target)
		}
		delete(want, declaration.Key())
	}
	if len(want) != 0 {
		t.Errorf("third-party owned declarations are missing: %v", want)
	}
}

func assertACMEPlan(t testing.TB, selected plan.Plan) {
	t.Helper()
	wantNodes := map[string]plan.Origin{
		acme.ReaderIdentity().String():  plan.Automatic,
		acme.DecoderIdentity().String(): plan.Automatic,
		acme.WriterIdentity().String():  plan.Requested,
	}
	for _, node := range selected.Nodes() {
		origin, wanted := wantNodes[node.Component]
		if !wanted {
			continue
		}
		if node.Origin != origin {
			t.Errorf("component %s origin = %v, want %v", node.Component, node.Origin, origin)
		}
		delete(wantNodes, node.Component)
	}
	if len(wantNodes) != 0 {
		t.Errorf("third-party Plan is missing components %v", wantNodes)
	}
	wantBoundaries := map[plan.BoundaryDirection]string{
		plan.InputBoundary:  acme.SourceIdentity().String(),
		plan.OutputBoundary: file.SinkIdentity().String(),
	}
	for _, boundary := range selected.Boundaries() {
		component, wanted := wantBoundaries[boundary.Direction]
		if !wanted {
			continue
		}
		if boundary.Kind != plan.ProviderBoundary || boundary.Component != component {
			t.Errorf("boundary %v = %#v, want component %s", boundary.Direction, boundary, component)
		}
		if boundary.Direction == plan.InputBoundary && (len(boundary.Selected) != 1 || boundary.Selected[0] != access.RandomRead) {
			t.Errorf("ACME input selection = %v, want random read", boundary.Selected)
		}
		delete(wantBoundaries, boundary.Direction)
	}
	if len(wantBoundaries) != 0 {
		t.Errorf("third-party Plan is missing boundaries %v", wantBoundaries)
	}
}

func localFileReference(t testing.TB, path string) access.Reference {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	value := filepath.ToSlash(absolute)
	if runtime.GOOS == "windows" && !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	reference, err := access.Parse((&url.URL{Scheme: "file", Path: value}).String())
	if err != nil {
		t.Fatal(err)
	}
	return reference
}
