package format

import (
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/plugin"
)

type fixtureFormatID struct{}
type fixturePacketizedFormatID struct{}
type fixtureCarrierID struct{}
type fixtureReadComponentID struct{}
type fixtureWriteComponentID struct{}
type fixtureConfigID struct{}
type fixtureUnit int

func TestFormatDeclaresIdentityAndOpenCarriers(t *testing.T) {
	declared := carrier.Define[fixtureCarrierID]()
	value, err := Define[fixtureFormatID]([]carrier.ID{declared})
	if err != nil {
		t.Fatal(err)
	}
	if !value.Valid() || value.Carriers()[0] != declared || value.Packetized() {
		t.Fatalf("format = %#v", value)
	}
	if _, err := Define[struct{}](nil); err == nil {
		t.Fatal("empty format identity accepted")
	}
}

func TestPacketizedFormatMayOmitCapabilityAlternative(t *testing.T) {
	value, err := DefinePacketized[fixturePacketizedFormatID](nil)
	if err != nil || !value.Packetized() {
		t.Fatalf("packetized format = %#v, %v", value, err)
	}
}

func TestDirectionTraitsOwnIndependentCapabilityAlternatives(t *testing.T) {
	value, err := Define[fixtureFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	schema := config.Struct[fixtureConfigID](func() struct{} { return struct{}{} }).Version("1").Build()
	readComponent := plugin.NewComponent[fixtureReadComponentID](plugin.Descriptor{DisplayName: "read"}, schema,
		Read(value, access.AnyOf(access.SequentialRead), access.AnyOf(access.RandomRead, access.StableSize)))
	writeComponent := plugin.NewComponent[fixtureWriteComponentID](plugin.Descriptor{DisplayName: "write"}, schema,
		Write(value, access.AnyOf(access.SequentialWrite)))
	read, readOK := ReadOf(readComponent)
	write, writeOK := WriteOf(writeComponent)
	if !readOK || !writeOK || !read.Valid() || !write.Valid() || read.Format().Identity() != value.Identity() || write.Format().Identity() != value.Identity() {
		t.Fatalf("traits = read %#v/%v, write %#v/%v", read, readOK, write, writeOK)
	}
	readRequirements := read.Requirements()
	writeRequirements := write.Requirements()
	if len(readRequirements.Alternatives) != 2 || len(writeRequirements.Alternatives) != 1 || writeRequirements.Alternatives[0].Capabilities[0] != access.SequentialWrite {
		t.Fatalf("requirements = read %#v, write %#v", readRequirements, writeRequirements)
	}
	readRequirements.Alternatives[0].Capabilities[0] = access.RandomWrite
	if read.Requirements().Alternatives[0].Capabilities[0] != access.SequentialRead {
		t.Fatal("ReadTrait leaked mutable requirements")
	}
}
