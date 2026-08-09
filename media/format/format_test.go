package format

import (
	"context"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/plugin"
)

type fixtureFormatID struct{}
type fixturePacketizedFormatID struct{}
type fixtureOtherFormatID struct{}
type fixtureCarrierID struct{}
type fixtureReadComponentID struct{}
type fixtureWriteComponentID struct{}
type fixtureConfigID struct{}
type fixtureUnit int

type fixtureInspectSession struct{ capabilities access.Capabilities }

func (s fixtureInspectSession) Capabilities() access.Capabilities { return s.capabilities }
func (fixtureInspectSession) Close() error                        { return nil }
func (fixtureInspectSession) ReadAt(context.Context, []byte, int64) (int, error) {
	return 0, nil
}

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

func TestReadTraitTransportsTypedInspectionThroughCompileContext(t *testing.T) {
	value, err := Define[fixtureFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := access.NewCapabilities(access.RandomRead)
	if err != nil {
		t.Fatal(err)
	}
	selection, ok := access.Select(capabilities, access.NewRequirements(access.AnyOf(access.RandomRead)))
	if !ok {
		t.Fatal("random read selection failed")
	}
	opening, err := access.NewOpening(access.SourceDirection, fixtureInspectSession{capabilities: capabilities}, selection, 0)
	if err != nil {
		t.Fatal(err)
	}
	type prepared struct{ Size int }
	called := 0
	schema := config.Struct[fixtureConfigID](func() struct{} { return struct{}{} }).Version("1").Build()
	component := plugin.NewComponent[fixtureReadComponentID](plugin.Descriptor{DisplayName: "inspected read"}, schema,
		ReadWithInspect(value, func(ctx InspectContext) (Inspection, error) {
			called++
			if !ctx.Valid() || ctx.Context() == nil || !ctx.Opening().Valid() {
				t.Fatal("InspectContext is invalid")
			}
			return NewInspection(value, prepared{Size: 44}), nil
		}, access.AnyOf(access.RandomRead)))
	trait, ok := ReadOf(component)
	if !ok || !trait.HasInspect() {
		t.Fatalf("read trait = %#v/%v", trait, ok)
	}
	inspection, err := trait.Inspect(NewInspectContext(context.Background(), opening))
	if err != nil || called != 1 {
		t.Fatalf("Inspect = %#v, %v; calls = %d", inspection, err, called)
	}
	compileContext, err := WithInspection(plugin.CompileContext{}, inspection)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := InspectionOf[prepared](compileContext, value)
	if !ok || got.Size != 44 {
		t.Fatalf("inspection = %#v/%v", got, ok)
	}
	other, err := Define[fixtureOtherFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := InspectionOf[prepared](compileContext, other); ok {
		t.Fatal("inspection accepted a different Format")
	}
}
