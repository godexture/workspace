package integration_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin/mp4"
	"github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/standard"
)

func TestMP4IlstToWAVMetadata(t *testing.T) {
	payload := make([]byte, 16)
	for index := 0; index < len(payload); index += 2 {
		binary.LittleEndian.PutUint16(payload[index:], uint16(index/2))
	}
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.mp4")
	outputPath := filepath.Join(directory, "output.wav")
	inputBytes := mp4PCMIlstFixture("sowt", payload,
		mp4FixtureIlstText(string([]byte{0xa9, 'n', 'a', 'm'}), "Title"),
		mp4FixtureIlstText(string([]byte{0xa9, 'w', 'r', 't'}), "Composer"),
	)
	if err := os.WriteFile(inputPath, inputBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	policy, ok := job.PolicyFor(job.Fast)
	if !ok {
		t.Fatal("fast policy is unavailable")
	}
	request := newMP4IlstWAVJob(t, inputPath, outputPath, policy)
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := instance.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	defer func() {
		if !closed {
			if err := prepared.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	predicted := prepared.Plan().PredictedMetadataLosses()
	if len(predicted) != 1 || predicted[0].Report.Loss.Key != tag.Composer().ID() {
		t.Fatalf("predicted metadata losses = %#v", predicted)
	}
	report := predicted[0].Report
	if predicted[0].Output != 0 || predicted[0].Node != "mux" || predicted[0].Component != wave.MuxerIdentity().String() || predicted[0].Port != "writes" || report.Carrier != wave.RIFFInfo() || report.Encoding != wave.InfoEncodingIdentity().String() || report.Block != "wave/info/0" || report.Loss.Kind != loss.Dropped || report.Loss.Detail != "wave.info-unrepresentable" || !report.Loss.Target.IsZero() || report.Loss.Mapping != 0 {
		t.Fatalf("predicted Composer loss = %#v", predicted[0])
	}
	source := report.Loss.Source
	if source.Carrier != mp4.IlstCarrier() || source.Encoding != mp4.IlstEncodingIdentity().String() || !strings.HasPrefix(source.Block, "mp4/ilst/") || source.Native != string([]byte{0xa9, 'w', 'r', 't'}) {
		t.Fatalf("predicted Composer source = %#v", source)
	}
	for _, value := range predicted {
		if value.Report.Loss.Key == tag.Title().ID() {
			t.Fatalf("common Title unexpectedly produced a loss report: %#v", value)
		}
	}
	if len(prepared.Plan().Warnings()) != 1 {
		t.Fatalf("plan warnings = %#v", prepared.Plan().Warnings())
	}
	result, err := prepared.Run(t.Context())
	if err != nil || !result.Succeeded() {
		t.Fatalf("MP4 to WAVE Run = %#v, %v", result, err)
	}
	if actual := result.ActualMetadataLosses(); len(actual) != 1 || actual[0].Report != predicted[0].Report {
		t.Fatalf("actual metadata losses = %#v, want %#v", actual, predicted)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true
	encoded, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	chunks := parseRIFF(t, encoded)
	if !hasINFOValue(chunks, "INAM", "Title") {
		t.Fatalf("WAVE metadata = %x", encoded)
	}
	if bytes.Contains(encoded, []byte("Composer")) {
		t.Fatal("WAVE output retained unsupported Composer text")
	}

	// Reinspect the committed WAVE through the standard file composition. The
	// demux plan exposes the owner attachment state and scope; the wire checks
	// above verify that Title survived and Composer did not.
	pcmPath := filepath.Join(directory, "reinspect.pcm")
	reinspect, err := standard.NewFileJob(outputPath, pcmPath)
	if err != nil {
		t.Fatal(err)
	}
	reinspected, err := instance.Prepare(t.Context(), reinspect)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reinspected.Close(); err != nil {
			t.Error(err)
		}
	}()
	var demuxFound bool
	for _, node := range reinspected.Plan().Nodes() {
		if node.Component != wave.DemuxerIdentity().String() {
			continue
		}
		demuxFound = true
		if len(node.Outputs) != 1 || node.Outputs[0].Descriptor.MetadataAvailability != "available" || node.Outputs[0].Descriptor.MetadataScope != "asset" {
			t.Fatalf("reinspected WAVE metadata descriptor = %#v", node.Outputs)
		}
	}
	if !demuxFound {
		t.Fatal("reinspection plan has no WAVE demux")
	}

	strict, ok := job.PolicyFor(job.Fast)
	if !ok {
		t.Fatal("fast policy is unavailable")
	}
	strict.Metadata = job.StrictMetadata
	strictRequest := newMP4IlstWAVJob(t, inputPath, filepath.Join(directory, "strict.wav"), strict)
	if _, err := instance.Prepare(t.Context(), strictRequest); err == nil {
		t.Fatal("StrictMetadata accepted the Composer loss")
	} else {
		items := host.Diagnostics(err)
		found := false
		for _, item := range items {
			if item.Code == "solve.metadata-loss" && item.Detail["key"] == tag.Composer().ID().String() && item.Detail["reason"] == "wave.info-unrepresentable" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("StrictMetadata diagnostics = %#v", items)
		}
	}
}

func TestMP4OpaqueIlstToWAVFailsClosed(t *testing.T) {
	payload := make([]byte, 8)
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "opaque.mp4")
	outputPath := filepath.Join(directory, "opaque.wav")
	// "----" is not one of the built-in iTunes semantic item atoms; its valid
	// data child is therefore retained by the ilst binding as an opaque block.
	inputBytes := mp4PCMIlstFixture("sowt", payload,
		mp4FixtureIlstText("----", "opaque"),
	)
	if err := os.WriteFile(inputPath, inputBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	policy, ok := job.PolicyFor(job.Fast)
	if !ok {
		t.Fatal("fast policy is unavailable")
	}
	request := newMP4IlstWAVJob(t, inputPath, outputPath, policy)
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Prepare(t.Context(), request); err == nil {
		t.Fatal("MP4 opaque ilst metadata was silently dropped into WAVE")
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("failed opaque conversion acquired output: %v", err)
	}
}

func TestStandardConvertReturnsActualMetadataLosses(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.mp4")
	outputPath := filepath.Join(directory, "output.wav")
	inputBytes := mp4PCMIlstFixture("sowt", make([]byte, 8),
		mp4FixtureIlstText(string([]byte{0xa9, 'n', 'a', 'm'}), "Title"),
		mp4FixtureIlstText(string([]byte{0xa9, 'w', 'r', 't'}), "Composer"),
	)
	if err := os.WriteFile(inputPath, inputBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := standard.Convert(t.Context(), inputPath, outputPath)
	if err != nil || !result.Succeeded() {
		t.Fatalf("standard.Convert = %#v, %v", result, err)
	}
	actual := result.ActualMetadataLosses()
	if len(actual) != 1 || actual[0].Report.Loss.Key != tag.Composer().ID() {
		t.Fatalf("actual metadata losses = %#v", actual)
	}
}

func newMP4IlstWAVJob(t testing.TB, inputPath, outputPath string, policy job.Policy) job.Job {
	t.Helper()
	input, err := job.InputFromReference(localFileReference(t, inputPath))
	if err != nil {
		t.Fatal(err)
	}
	output, err := job.OutputToReference(localFileReference(t, outputPath))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("demux", mp4.DemuxerIdentity(), config.NewPatch()),
			job.NewNode("mux", wave.MuxerIdentity(), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("demux", "packets"), job.At("mux", "packets"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New([]job.Input{input}, []job.Output{output}, graph, job.WithPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}
	return request
}
