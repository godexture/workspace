package cli

import (
	"strings"
	"testing"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/pipeline"
)

func TestWriteConversionStartRendersSeparateAuxiliaryChain(t *testing.T) {
	description := pipeline.Description{
		Nodes: []pipeline.NodeDescription{
			{ID: "demuxer", Role: manifest.RoleDemuxer, Plugin: "mp3"},
			{ID: "decoder", Role: manifest.RoleDecoder, Plugin: "mp3"},
			{ID: "filter:convolver", Role: manifest.RoleFilter, Plugin: "convolver"},
			{ID: "encoder", Role: manifest.RoleEncoder, Plugin: "flac"},
			{ID: "muxer", Role: manifest.RoleMuxer, Plugin: "flac"},
			{ID: "aux:ir:demuxer", Role: manifest.RoleDemuxer, Plugin: "wav"},
			{ID: "aux:ir:decoder", Role: manifest.RoleDecoder, Plugin: "pcm"},
			{ID: "aux:ir:filter:resample", Role: manifest.RoleFilter, Plugin: "resample"},
		},
		Edges: []pipeline.EdgeDescription{
			{FromNode: "demuxer", FromPort: "out", ToNode: "decoder", ToPort: "in"},
			{FromNode: "decoder", FromPort: "out", ToNode: "filter:convolver", ToPort: "in"},
			{FromNode: "filter:convolver", FromPort: "out", ToNode: "encoder", ToPort: "in"},
			{FromNode: "encoder", FromPort: "out", ToNode: "muxer", ToPort: "in"},
			{FromNode: "aux:ir:demuxer", FromPort: "out", ToNode: "aux:ir:decoder", ToPort: "in"},
			{FromNode: "aux:ir:decoder", FromPort: "out", ToNode: "aux:ir:filter:resample", ToPort: "in"},
			{FromNode: "aux:ir:filter:resample", FromPort: "out", ToNode: "filter:convolver", ToPort: "ir"},
		},
	}

	var output strings.Builder
	if err := writeConversionStart(&output, description); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "main: demuxer(mp3) -> decoder(mp3) -> filter:convolver(convolver)") {
		t.Fatalf("main chain missing from output:\n%s", text)
	}
	if !strings.Contains(text, "ir: aux:ir:demuxer(wav) -> aux:ir:decoder(pcm) -> aux:ir:filter:resample(resample) -> filter:convolver.ir") {
		t.Fatalf("auxiliary chain missing from output:\n%s", text)
	}
}
