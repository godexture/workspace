package wave

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plan"
	fileplugin "github.com/godexture/godec/plugin/file"
	"github.com/godexture/godec/plugin/pcm/linear"
)

func TestWAVEFileToRawPCMEndToEnd(t *testing.T) {
	payload := []byte{
		0x01, 0x00, 0xff, 0x7f,
		0xff, 0xff, 0x00, 0x80,
		0x34, 0x12, 0xcc, 0xed,
		0x00, 0x00, 0x01, 0x00,
	}
	for _, preset := range []job.Preset{job.Fast, job.Realtime} {
		t.Run(preset.String(), func(t *testing.T) {
			directory := t.TempDir()
			inputPath := filepath.Join(directory, "input.wav")
			outputPath := filepath.Join(directory, "output.pcm")
			if err := os.WriteFile(inputPath, testWAVE(payload, 2, 48_000, testChunk{id: "JUNK", payload: []byte{0xff}}), 0o600); err != nil {
				t.Fatal(err)
			}
			set := linear.Set().Add(fileplugin.Plugin()).Add(Plugin())
			instance, err := host.New(
				host.Plugins(set),
				host.PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
			)
			if err != nil {
				t.Fatal(err)
			}
			patch := config.NewPatch().
				SetText("rate", strconv.Itoa(48_000)).
				SetText("validBits", "16").
				SetText("layout", "stereo").
				SetText("endian", "little").
				SetText("chunkSamples", "2")
			requested, err := job.NewGraph(
				[]job.Node{
					job.NewNode("demux", DemuxerIdentity(), config.NewPatch()),
					job.NewNode("parser", linear.ParserIdentity(), patch),
					job.NewNode("decoder", linear.DecoderIdentity(), patch),
					job.NewNode("encoder", linear.EncoderIdentity(), patch),
					job.NewNode("writer", linear.WriterIdentity(), patch),
				},
				[]job.Edge{
					job.Connect(job.At("demux", "chunks"), job.At("parser", "chunks")),
					job.Connect(job.At("parser", "packets"), job.At("decoder", "packets")),
					job.Connect(job.At("decoder", "frames"), job.At("encoder", "frames")),
					job.Connect(job.At("encoder", "packets"), job.At("writer", "packets")),
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			input, err := job.InputFromReference(fileReference(t, inputPath))
			if err != nil {
				t.Fatal(err)
			}
			output, err := job.OutputToReference(fileReference(t, outputPath))
			if err != nil {
				t.Fatal(err)
			}
			policy, ok := job.PolicyFor(preset)
			if !ok {
				t.Fatalf("policy %s is unavailable", preset)
			}
			request, err := job.New([]job.Input{input}, []job.Output{output}, requested, job.WithPolicy(policy))
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := instance.Prepare(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := prepared.Close(); err != nil {
					t.Error(err)
				}
			}()
			assertWAVEReadPlan(t, prepared.Plan())
			result, err := prepared.Run(context.Background())
			if err != nil || !result.Succeeded() {
				t.Fatalf("Run result = %#v, error %v", result, err)
			}
			decoded, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(decoded, payload) {
				t.Fatalf("raw PCM = %v, want %v", decoded, payload)
			}
		})
	}
}

func assertWAVEReadPlan(t *testing.T, value plan.Plan) {
	t.Helper()
	for _, boundary := range value.Boundaries() {
		if boundary.Direction == plan.InputBoundary {
			if len(boundary.Selected) != 1 || boundary.Selected[0] != access.RandomRead {
				t.Fatalf("WAVE input selection = %#v", boundary)
			}
		}
	}
	for _, node := range value.Nodes() {
		if node.ID != "demux" {
			continue
		}
		if len(node.Outputs) != 1 || node.Outputs[0].Descriptor.Schema != mediaformat.Chunks().Identity().String() || node.Outputs[0].Descriptor.TimeBaseNumerator != 1 || node.Outputs[0].Descriptor.TimeBaseDenominator != 48_000 {
			t.Fatalf("WAVE demux descriptor = %#v", node.Outputs)
		}
		return
	}
	t.Fatal("WAVE demux node is absent from Plan")
}

func fileReference(t *testing.T, path string) access.Reference {
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
