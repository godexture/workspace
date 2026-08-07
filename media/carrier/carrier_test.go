package carrier

import (
	"strings"
	"testing"
)

type payloadID struct{}

func TestDefineDerivesAnOpenCarrierIdentity(t *testing.T) {
	id := Define[payloadID]()
	if !id.Valid() || id.Name() != "payloadID" || !strings.HasSuffix(id.PackagePath(), "media/carrier") {
		t.Fatalf("carrier identity = %q", id)
	}
	if Define[payloadID]() != id {
		t.Fatal("carrier identity is not stable")
	}
}

func TestInvalidMarkerProducesAnInvalidIdentity(t *testing.T) {
	if Define[struct{}]().Valid() {
		t.Fatal("anonymous carrier marker was accepted")
	}
}
