package mp4

import "testing"

func TestMP4Definition(t *testing.T) {
	value := MP4()
	if !value.Valid() || !value.Packetized() {
		t.Fatalf("MP4() = %#v, want valid packetized Format", value)
	}
	extensions := value.Extensions()
	if len(extensions) != 1 || extensions[0].String() != "mp4" {
		t.Fatalf("MP4 extensions = %v, want [mp4]", extensions)
	}
	if !value.Same(MP4()) {
		t.Fatal("MP4 declaration is not stable")
	}
}
