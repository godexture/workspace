package manifest

import (
	"strings"
	"testing"

	"github.com/godexture/core/domain/media"
)

func TestAudioConstraintMatchesWildcardAndConcreteFields(t *testing.T) {
	t.Parallel()
	stream := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecLPCM,
			Audio: media.AudioAttributes{
				SampleRate:    48000,
				Format:        media.SampleFormatS24,
				BitsPerSample: 20,
				ChannelLayout: media.LayoutStereo2_0,
			},
		},
	}
	constraint := &AudioConstraint{
		Codecs:      []media.CodecID{media.CodecLPCM},
		SampleRates: IntConstraint{Values: []int{44100, 48000}},
		Channels:    IntConstraint{Min: 1, Max: 2},
		Layouts:     []media.ChannelLayout{media.LayoutStereo2_0},
		SampleFormats: []SampleFormatConstraint{{
			Format:        media.SampleFormatS24,
			BitsPerSample: IntConstraint{Min: 17, Max: 24},
		}},
	}
	if !constraint.Match(stream) {
		t.Fatalf("constraint rejected matching stream: %v", constraint.Diagnose(stream))
	}

	stream.Audio.BitsPerSample = 16
	if constraint.Match(stream) {
		t.Fatal("constraint accepted invalid bit depth")
	}
	if err := constraint.Diagnose(stream); err == nil {
		t.Fatal("Diagnose() returned nil for rejected stream")
	}
}

func TestAudioConstraintEmptyFieldsAreWildcards(t *testing.T) {
	t.Parallel()
	constraint := &AudioConstraint{}
	stream := media.StreamInfo{Type: media.MediaAudio}
	if !constraint.Match(stream) {
		t.Fatalf("empty constraint rejected stream: %v", constraint.Diagnose(stream))
	}
}

func TestAudioConstraintRejectsNonAudio(t *testing.T) {
	t.Parallel()
	constraint := &AudioConstraint{}
	if constraint.Match(media.StreamInfo{Type: media.MediaVideo}) {
		t.Fatal("audio constraint accepted video stream")
	}
}

func TestAudioConstraintDiagnoseUsesEffectiveBitDepth(t *testing.T) {
	t.Parallel()
	constraint := &AudioConstraint{SampleFormats: []SampleFormatConstraint{{
		Format:        media.SampleFormatS16,
		BitsPerSample: IntConstraint{Values: []int{24}},
	}}}
	stream := media.StreamInfo{Type: media.MediaAudio, MediaAttributes: media.MediaAttributes{Audio: media.AudioAttributes{Format: media.SampleFormatS16}}}
	err := constraint.Diagnose(stream)
	if err == nil || !strings.Contains(err.Error(), "16 bits") {
		t.Fatalf("Diagnose() error = %v, want effective bit depth", err)
	}
}
