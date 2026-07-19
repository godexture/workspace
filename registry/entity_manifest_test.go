package registry

import (
	"io"
	"strings"
	"testing"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

type acceptAllCapability struct{}
type pointerCapability struct{}

func (acceptAllCapability) MediaType() media.MediaType     { return media.MediaAudio }
func (acceptAllCapability) Match(media.StreamInfo) bool    { return true }
func (acceptAllCapability) Diagnose(media.StreamInfo) bool { return true }

func (*pointerCapability) MediaType() media.MediaType     { return media.MediaAudio }
func (*pointerCapability) Match(media.StreamInfo) bool    { return true }
func (*pointerCapability) Diagnose(media.StreamInfo) bool { return true }

func TestManifestValidationRejectsIncompleteContracts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		validate func() error
		contains string
	}{
		{
			name:     "muxer name",
			validate: func() error { return (MuxerManifest{}).Validate() },
			contains: "name",
		},
		{
			name: "muxer factory",
			validate: func() error {
				return (MuxerManifest{BaseManifest: BaseManifest{Name: "muxer"}}).Validate()
			},
			contains: "factory",
		},
		{
			name: "demuxer probe",
			validate: func() error {
				return (DemuxerManifest{
					BaseManifest: BaseManifest{Name: "demuxer"},
					Factory:      func(io.Reader, Configuration) (node.Demuxer, error) { return nil, nil },
				}).Validate()
			},
			contains: "probe",
		},
		{
			name: "encoder capabilities",
			validate: func() error {
				return (EncoderManifest{
					TransformManifest: TransformManifest{BaseManifest: BaseManifest{Name: "encoder"}},
				}).Validate()
			},
			contains: "capability",
		},
		{
			name: "encoder supports",
			validate: func() error {
				return (EncoderManifest{
					TransformManifest: TransformManifest{
						BaseManifest: BaseManifest{Name: "encoder"},
						Capabilities: []manifest.Capability{acceptAllCapability{}},
					},
					Factory: func(media.StreamInfo, media.CodecID, TransformFactoryOptions) (node.Encoder, error) {
						return nil, nil
					},
				}).Validate()
			},
			contains: "codec matcher",
		},
		{
			name: "typed nil capability",
			validate: func() error {
				return (DecoderManifest{
					TransformManifest: TransformManifest{
						BaseManifest: BaseManifest{Name: "decoder"},
						Capabilities: []manifest.Capability{
							(*pointerCapability)(nil),
						},
					},
					Factory: func(media.StreamInfo, TransformFactoryOptions) (node.Decoder, error) {
						return nil, nil
					},
				}).Validate()
			},
			contains: "nil",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.validate()
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("Validate() error = %v, want text %q", err, test.contains)
			}
		})
	}
}
