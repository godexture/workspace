package resource

import (
	"testing"
	"time"
)

func TestGrantSatisfiesEveryRequestedDimension(t *testing.T) {
	request := Request{Memory: 1024, Temporary: 4096, Workers: 2, Queue: 8}
	if !(Grant{Memory: 1024, Temporary: 4096, Workers: 2, Queue: 8}).Satisfies(request) {
		t.Fatal("exact grant did not satisfy request")
	}
	if (Grant{Memory: 1024, Temporary: 4096, Workers: 1, Queue: 8}).Satisfies(request) {
		t.Fatal("undersized worker grant satisfied request")
	}
}

func TestEstimateRejectsNegativeDurationOrInvalidConfidence(t *testing.T) {
	if (Estimate{Latency: -time.Nanosecond}).Valid() {
		t.Fatal("negative latency estimate is valid")
	}
	if (Estimate{Confidence: Confidence(99)}).Valid() {
		t.Fatal("unknown confidence is valid")
	}
	if !(Estimate{Confidence: BoundedConfidence}).Valid() {
		t.Fatal("bounded estimate is invalid")
	}
}
