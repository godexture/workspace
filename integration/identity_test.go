package integration_test

import (
	"testing"

	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/file"
	"github.com/godexture/godec/plugin/id3"
	"github.com/godexture/godec/plugin/pcm/linear"
	"github.com/godexture/godec/plugin/wave"
)

func TestOfficialComponentMarkerIdentities(t *testing.T) {
	tests := []struct {
		identity plugin.Identity
		path     string
		name     string
	}{
		{file.SourceIdentity(), "github.com/godexture/godec/plugin/file", "sourceID"},
		{file.SinkIdentity(), "github.com/godexture/godec/plugin/file", "sinkID"},
		{linear.ReaderIdentity(), "github.com/godexture/godec/plugin/pcm/linear", "readerID"},
		{linear.ParserIdentity(), "github.com/godexture/godec/plugin/pcm/linear", "parserID"},
		{linear.DecoderIdentity(sample.S16), "github.com/godexture/godec/plugin/pcm/linear", "decoderS16ID"},
		{linear.DecoderIdentity(sample.S24), "github.com/godexture/godec/plugin/pcm/linear", "decoderS32ID"},
		{linear.EncoderIdentity(sample.S16), "github.com/godexture/godec/plugin/pcm/linear", "encoderS16ID"},
		{linear.EncoderIdentity(sample.F64), "github.com/godexture/godec/plugin/pcm/linear", "encoderF64ID"},
		{linear.WriterIdentity(), "github.com/godexture/godec/plugin/pcm/linear", "writerID"},
		{id3.V1EncodingIdentity(), "github.com/godexture/godec/plugin/id3", "v1ID"},
		{wave.DemuxerIdentity(), "github.com/godexture/godec/plugin/wave", "demuxerID"},
		{wave.MuxerIdentity(), "github.com/godexture/godec/plugin/wave", "muxerID"},
		{wave.InfoEncodingIdentity(), "github.com/godexture/godec/plugin/wave", "infoID"},
	}
	for _, test := range tests {
		if test.identity.PackagePath() != test.path || test.identity.Name() != test.name {
			t.Errorf("identity %s = path %q name %q, want %q %q", test.identity, test.identity.PackagePath(), test.identity.Name(), test.path, test.name)
		}
	}
}
