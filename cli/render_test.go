package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/standard"
)

type renderLossCarrierID struct{}
type renderLossSourceCarrierID struct{}
type renderLossKeyID struct{}
type renderLossTargetID struct{}

func TestRenderPlanIncludesPredictedMetadataLossDetails(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	outputPath := filepath.Join(directory, "output.wav")
	if err := os.WriteFile(inputPath, pcmWave(1, 48_000, 16, []byte{1, 0}), 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := standard.NewFileJob(inputPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	selected, err := instance.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	description := selected.Description()
	producer, boundary, ok := firstOutputLossLocation(description)
	if !ok {
		t.Fatal("planned graph has no producer-to-output edge")
	}
	keyID := key.Define[renderLossKeyID, string]().ID()
	targetID := key.Define[renderLossTargetID, string]().ID()
	description.PredictedMetadataLosses = []plan.PredictedMetadataLoss{{
		Output: boundary.Choice, Node: producer.node, Component: producer.component, Port: producer.port,
		Report: loss.Report{
			Carrier: carrier.Define[renderLossCarrierID](), Encoding: "fixture.encoding", Block: "fixture/block",
			Loss: loss.Loss{Key: keyID, Kind: loss.Converted, Native: "fixture-native", Target: targetID, Mapping: loss.Lossless, Detail: "fixture.mapping", Source: loss.Origin{
				Carrier: carrier.Define[renderLossSourceCarrierID](), Encoding: "fixture.source", Block: "fixture/source", Native: "source-native",
			}},
		},
	}}
	withLoss, err := plan.New(description)
	if err != nil {
		t.Fatal(err)
	}
	var rendered bytes.Buffer
	if err := renderPlan(&rendered, withLoss); err != nil {
		t.Fatal(err)
	}
	want := "metadata-loss output=" + strconv.Itoa(boundary.Choice) +
		" node=" + producer.node + " component=" + producer.component + " port=" + producer.port +
		" block=fixture/block carrier=" + carrier.Define[renderLossCarrierID]().String() +
		" encoding=fixture.encoding key=" + keyID.String() + " kind=converted mapping=lossless native=fixture-native reason=fixture.mapping" +
		" sourceBlock=fixture/source sourceCarrier=" + carrier.Define[renderLossSourceCarrierID]().String() +
		" sourceEncoding=fixture.source sourceNative=source-native target=" + targetID.String()
	if !strings.Contains(rendered.String(), want) {
		t.Fatalf("rendered plan omitted metadata loss details:\n%s\nwant %q", rendered.String(), want)
	}
}

type renderLossProducer struct {
	node      string
	component string
	port      string
}

func firstOutputLossLocation(description plan.Description) (renderLossProducer, plan.Boundary, bool) {
	for _, boundary := range description.Boundaries {
		if boundary.Direction != plan.OutputBoundary {
			continue
		}
		for _, edge := range description.Edges {
			if edge.ToNode != boundary.Node || edge.ToPort != boundary.Port {
				continue
			}
			for _, node := range description.Nodes {
				if node.ID == edge.FromNode {
					return renderLossProducer{node: node.ID, component: node.Component, port: edge.FromPort}, boundary, true
				}
			}
		}
	}
	return renderLossProducer{}, plan.Boundary{}, false
}
