package host

import (
	"context"
	"errors"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plan"
)

type commitMetadataCarrierID struct{}
type commitMetadataSourceCarrierID struct{}
type commitMetadataKeyID struct{}

type commitTransaction struct{ err error }

func (t commitTransaction) PrepareCommit(context.Context) error { return nil }
func (t commitTransaction) Commit(context.Context) error        { return t.err }
func (commitTransaction) Abort(context.Context) error           { return nil }

func TestCommittedOutputAloneMakesMetadataLossActual(t *testing.T) {
	targetCarrier := carrier.Define[commitMetadataCarrierID]()
	sourceCarrier := carrier.Define[commitMetadataSourceCarrierID]()
	key := key.Define[commitMetadataKeyID, string]().ID()
	losses := []plan.PredictedMetadataLoss{
		{Output: 0, Node: "writer", Component: "fixture.writer", Port: "writes", Report: loss.Report{
			Carrier: targetCarrier, Encoding: "fixture.encoding", Block: "fixture/first",
			Loss: loss.Loss{Key: key, Kind: loss.Dropped, Native: "fixture/native", Detail: "fixture.first", Source: loss.Origin{
				Carrier: sourceCarrier, Encoding: "fixture.source", Block: "fixture/source", Native: "fixture/source-native",
			}},
		}},
		{Output: 1, Node: "writer", Component: "fixture.writer", Port: "writes", Report: loss.Report{
			Carrier: targetCarrier, Encoding: "fixture.encoding", Block: "fixture/second",
			Loss: loss.Loss{Key: key, Kind: loss.Dropped, Detail: "fixture.second"},
		}},
	}
	runner := &runner{
		ctx:            context.Background(),
		ledger:         journal.NewLedger(),
		diag:           &diagnosticLog{},
		metadataLosses: losses,
		result: Result{Outputs: []OutputOutcome{
			{Choice: 0, Node: "sink-a", State: OutputPending},
			{Choice: 1, Node: "sink-b", State: OutputPending},
		}},
		outputs: []*outputRuntime{
			{outcome: 0, class: access.AtomicReplace, transaction: commitTransaction{}},
			{outcome: 1, class: access.AtomicReplace, transaction: commitTransaction{err: errors.New("second commit failed")}},
		},
	}
	if failure := runner.finishOutputs(); failure == nil || failure.Phase != CommitPhase {
		t.Fatalf("commit failure = %#v", failure)
	}
	if runner.result.Outputs[0].State != OutputCommitted || len(runner.result.Outputs[0].MetadataLosses) != 1 {
		t.Fatalf("first outcome = %#v", runner.result.Outputs[0])
	}
	if runner.result.Outputs[1].State != OutputUnknown || len(runner.result.Outputs[1].MetadataLosses) != 0 {
		t.Fatalf("second outcome = %#v", runner.result.Outputs[1])
	}
	if actual := runner.result.ActualMetadataLosses(); len(actual) != 1 || actual[0] != (ActualMetadataLoss{
		Output: losses[0].Output, Node: losses[0].Node, Component: losses[0].Component, Port: losses[0].Port, Report: losses[0].Report,
	}) {
		t.Fatalf("actual metadata losses = %#v", actual)
	}
	diagnostics := runner.diag.snapshot()
	if len(diagnostics) != 1 || diagnostics[0].Code != "host.metadata-loss" || diagnostics[0].Detail["block"] != "fixture/first" || diagnostics[0].Detail["mapping"] != "none" || diagnostics[0].Detail["sourceCarrier"] != sourceCarrier.String() || diagnostics[0].Detail["sourceEncoding"] != "fixture.source" || diagnostics[0].Detail["sourceBlock"] != "fixture/source" || diagnostics[0].Detail["sourceNative"] != "fixture/source-native" {
		t.Fatalf("metadata diagnostics = %#v", diagnostics)
	}
}
