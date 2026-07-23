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

func (acceptAllCapability) Match(media.StreamInfo) bool     { return true }
func (acceptAllCapability) Diagnose(media.StreamInfo) error { return nil }

func (*pointerCapability) Match(media.StreamInfo) bool     { return true }
func (*pointerCapability) Diagnose(media.StreamInfo) error { return nil }

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
			contains: "input requirements",
		},
		{
			name: "encoder supports",
			validate: func() error {
				return (EncoderManifest{
					TransformManifest: TransformManifest{
						BaseManifest:      BaseManifest{Name: "encoder"},
						InputRequirements: StaticRequirements(acceptAllCapability{}),
					},
					Factory: func(media.StreamInfo, media.CodecID, TransformFactoryOptions) (node.Encoder, media.StreamInfo, error) {
						return nil, media.StreamInfo{}, nil
					},
				}).Validate()
			},
			contains: "no codecs",
		},
		{
			name: "typed nil capability",
			validate: func() error {
				manifest := DecoderManifest{
					TransformManifest: TransformManifest{
						BaseManifest:      BaseManifest{Name: "decoder"},
						InputRequirements: StaticRequirements((*pointerCapability)(nil)),
					},
					Factory: func(media.StreamInfo, TransformFactoryOptions) (node.Decoder, media.StreamInfo, error) {
						return nil, media.StreamInfo{}, nil
					},
				}
				if err := manifest.Validate(); err != nil {
					return err
				}
				_, err := manifest.Requirements(media.CodecID(""), nil)
				return err
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

func TestTransformManifestUsesTargetAwareInputRequirements(t *testing.T) {
	t.Parallel()
	manifest := TransformManifest{
		BaseManifest: BaseManifest{Name: "target-aware"},
		InputRequirements: func(target media.CodecID, _ Configuration) ([]manifest.Capability, error) {
			return []manifest.Capability{&manifest.AudioConstraint{Codecs: []media.CodecID{target}}}, nil
		},
	}
	stream := media.StreamInfo{Type: media.MediaAudio, MediaAttributes: media.MediaAttributes{Codec: media.CodecFLAC}}
	accepted, err := manifest.Accept(stream, media.CodecFLAC, nil)
	if err != nil || !accepted {
		t.Fatalf("FLAC target accepted = %t, error = %v", accepted, err)
	}
	accepted, err = manifest.Accept(stream, media.CodecLPCM, nil)
	if err != nil || accepted {
		t.Fatalf("LPCM target accepted = %t, error = %v", accepted, err)
	}
}
