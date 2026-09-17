package mp3

import (
	"testing"

	"github.com/godexture/godec/host"
	"github.com/godexture/godec/media/format"
)

func TestDefinitionAndReadTrait(t *testing.T) {
	value := MP3()
	if !value.Valid() || value.Packetized() || len(value.Extensions()) != 1 || value.Extensions()[0].String() != "mp3" {
		t.Fatalf("MP3 format = %#v", value)
	}
	carriers := value.Carriers()
	if len(carriers) != 2 || carriers[0] == carriers[1] || carriers[0] != LeadingID3v2() || carriers[1] != TrailingID3v1() {
		t.Fatalf("MP3 carriers = %#v", carriers)
	}
	if CodecTag() != format.NewTag("mp3", "audio") {
		t.Fatalf("CodecTag() = %q", CodecTag())
	}
	if len(Plugin().Components()) != 2 || len(Set().Components()) != 2 {
		t.Fatal("MP3 composition does not contain reader and parser")
	}
	if _, err := host.New(host.Plugins(Set())); err != nil {
		t.Fatal(err)
	}
	reader, parser := Plugin().Components()[0], Plugin().Components()[1]
	if reader.Identity() == parser.Identity() {
		t.Fatal("reader and parser identities collide")
	}
	read, ok := format.ReadOf(reader)
	if !ok || !read.Valid() || !read.HasProbe() || !read.HasInspect() {
		t.Fatal("reader does not expose a valid probe/inspect read trait")
	}
	requirements := read.Requirements()
	if len(requirements.Alternatives) != 1 || len(requirements.Alternatives[0].Capabilities) != 2 {
		t.Fatalf("MP3 read requirements = %#v", requirements)
	}
	if parserShape().Inputs[0].ID() != "chunks" || parserShape().Outputs[0].ID() != "packets" || readerShape().Inputs[0].ID() != "bytes" || readerShape().Outputs[0].ID() != "chunks" {
		t.Fatal("MP3 component shapes changed")
	}
}
