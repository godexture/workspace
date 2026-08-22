package wave

import (
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

// TestMuxRequestsTheDescriptionItCanWrite keeps a convertible PCM shape out of
// the failure path. WAVE stores interleaved little-endian samples, so anything
// else becomes a requirement the planner can satisfy with a codec bridge.
func TestMuxRequestsTheDescriptionItCanWrite(t *testing.T) {
	writable := sample.Description{Format: sample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: sample.Stereo, Endian: sample.LittleEndian}
	for _, testCase := range []struct {
		name        string
		description sample.Description
		requires    bool
	}{
		{name: "little endian is written", description: writable},
		{
			name:        "big endian is requested as little endian",
			description: sample.Description{Format: sample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: sample.Stereo, Endian: sample.BigEndian},
			requires:    true,
		},
		{
			name:        "planar is requested as interleaved",
			description: sample.Description{Format: sample.S16Planar, ValidBits: 16, Rate: 48_000, Layout: sample.Stereo, Endian: sample.NoEndian},
			requires:    true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			properties, err := testCase.description.Properties()
			if err != nil {
				t.Fatal(err)
			}
			input := stream.MustDescriptor("wave", codec.Packets().Descriptor(), timing.MustBase(1, int64(testCase.description.Rate)), properties)
			component := muxerComponent()
			resolved, err := component.Resolve(config.NewPatch())
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := plugin.Compile(component, plugin.CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("packets", input)))
			if err != nil {
				t.Fatal(err)
			}
			requirements, ok := plugin.RequirementsOf[stream.Descriptor](compiled)
			if !ok {
				t.Fatal("compilation does not carry stream requirements")
			}
			if !testCase.requires {
				if len(requirements) != 0 {
					t.Fatalf("writable description produced requirements %#v", requirements)
				}
				return
			}
			if len(requirements) != 1 || requirements[0].Port() != "packets" {
				t.Fatalf("requirements = %#v", requirements)
			}
			desired, present := requirements[0].Need().Desired()
			if !present {
				t.Fatal("requirement carries no desired descriptor")
			}
			got, err := sample.FromProperties(desired.Properties())
			if err != nil || got != writable {
				t.Fatalf("desired description = %#v, %v", got, err)
			}
		})
	}
}
