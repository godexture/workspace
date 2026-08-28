package loss

import (
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
)

type testCarrierID struct{}
type testSourceCarrierID struct{}
type testKeyID struct{}
type testTargetID struct{}

var (
	testCarrier       = carrier.Define[testCarrierID]()
	testSourceCarrier = carrier.Define[testSourceCarrierID]()
	testKey           = key.Define[testKeyID, string]().ID()
	testTarget        = key.Define[testTargetID, string]().ID()
)

func TestLossValidatesItsKindSpecificContract(t *testing.T) {
	for _, test := range []struct {
		name  string
		value Loss
		valid bool
	}{
		{name: "dropped", value: Loss{Key: testKey, Kind: Dropped, Detail: "fixture.drop"}, valid: true},
		{name: "folded", value: Loss{Key: testKey, Kind: Folded, Native: "TIT2", Detail: "fixture.fold"}, valid: true},
		{name: "converted", value: Loss{Key: testKey, Kind: Converted, Target: testTarget, Mapping: Approximate, Detail: "fixture.convert"}, valid: true},
		{name: "converted without target", value: Loss{Key: testKey, Kind: Converted, Mapping: Lossless, Detail: "fixture.convert"}},
		{name: "converted without mapping", value: Loss{Key: testKey, Kind: Converted, Target: testTarget, Detail: "fixture.convert"}},
		{name: "dropped with target", value: Loss{Key: testKey, Kind: Dropped, Target: testTarget, Detail: "fixture.drop"}},
		{name: "dropped with mapping", value: Loss{Key: testKey, Kind: Dropped, Mapping: Lossless, Detail: "fixture.drop"}},
		{name: "blank detail", value: Loss{Key: testKey, Kind: Dropped, Detail: " \t "}},
		{name: "partial origin", value: Loss{Key: testKey, Kind: Dropped, Detail: "fixture.drop", Source: Origin{Carrier: testSourceCarrier, Encoding: "fixture.source"}}},
	} {
		if got := test.value.Valid(); got != test.valid {
			t.Errorf("%s valid = %v, want %v", test.name, got, test.valid)
		}
	}
}

func TestLossinessKeepsLosslessConversionVisibleWithoutCallingItLossy(t *testing.T) {
	if (Loss{Key: testKey, Kind: Converted, Target: testTarget, Mapping: Lossless, Detail: "fixture.lossless"}).Lossy() {
		t.Fatal("lossless conversion is lossy")
	}
	if !(Loss{Key: testKey, Kind: Converted, Target: testTarget, Mapping: Ambiguous, Detail: "fixture.ambiguous"}).Lossy() {
		t.Fatal("ambiguous conversion is not lossy")
	}
	if !(Loss{Key: testKey, Kind: Dropped, Detail: "fixture.drop"}).Lossy() {
		t.Fatal("dropped value is not lossy")
	}
}

func TestReportAndOriginRequireStableTargetIdentifiers(t *testing.T) {
	value := Loss{Key: testKey, Kind: Dropped, Detail: "fixture.drop", Source: Origin{
		Carrier: testSourceCarrier, Encoding: "fixture.source", Block: "fixture/source", Native: "native",
	}}
	if !value.Valid() {
		t.Fatal("fully described loss is invalid")
	}
	for _, origin := range []Origin{
		{Carrier: testSourceCarrier, Encoding: " \t", Block: "fixture/source"},
		{Carrier: testSourceCarrier, Encoding: "fixture.source", Block: " \t"},
	} {
		if origin.Valid() {
			t.Fatalf("whitespace origin accepted: %#v", origin)
		}
	}
	for _, report := range []Report{
		{Carrier: testCarrier, Encoding: " \t", Block: "fixture/block", Loss: value},
		{Carrier: testCarrier, Encoding: "fixture.encoding", Block: " \t", Loss: value},
	} {
		if report.Valid() {
			t.Fatalf("whitespace report accepted: %#v", report)
		}
	}
	if !(Report{Carrier: testCarrier, Encoding: "fixture.encoding", Block: "fixture/block", Loss: value}).Valid() {
		t.Fatal("fully described report is invalid")
	}
}
