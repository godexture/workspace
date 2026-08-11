package integration_test

import (
	"testing"

	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/file"
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
		{linear.DecoderIdentity(), "github.com/godexture/godec/plugin/pcm/linear", "decoderID"},
		{linear.EncoderIdentity(), "github.com/godexture/godec/plugin/pcm/linear", "encoderID"},
		{linear.WriterIdentity(), "github.com/godexture/godec/plugin/pcm/linear", "writerID"},
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
