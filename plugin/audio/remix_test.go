package audio

import (
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/sample"
)

func remixSettings(layout sample.Layout) remixConfig {
	return remixConfig{
		Layout:    layout,
		Center:    config.Some[config.DecibelValue](halfPower),
		Surround:  config.Some[config.DecibelValue](halfPower),
		LowEnd:    config.None[config.DecibelValue](),
		Normalize: false,
	}
}

func mix(from sample.Layout, settings remixConfig, in [][]float32) [][]float32 {
	out := make([][]float32, settings.Layout.Count())
	for channel := range out {
		out[channel] = make([]float32, len(in[0]))
	}
	newRemixMatrix(from, settings).Produce(out, in)
	return out
}

// A channel the target also has arrives unchanged, whatever order the two
// layouts happen to store their channels in.
func TestRemixMatchesChannelsByPositionRatherThanByOrder(t *testing.T) {
	from := sample.Positions(sample.FrontLeft, sample.FrontRight, sample.FrontCenter)
	out := mix(from, remixSettings(sample.Positions(sample.FrontCenter, sample.FrontLeft, sample.FrontRight)),
		[][]float32{{1}, {0.5}, {0.25}})
	// The target stores its channels in mask order, so front left comes first
	// and the centre it also has arrives untouched.
	want := []float32{1, 0.5, 0.25}
	for channel, value := range want {
		if out[channel][0] != value {
			t.Fatalf("channel %d = %v, want %v (got %v)", channel, out[channel][0], value, out)
		}
	}
}

// A centre the target does not have is spread over the front pair at half
// power, so the two together carry what the one did.
func TestRemixFoldsACentreIntoTheFrontPair(t *testing.T) {
	from := sample.Positions(sample.FrontLeft, sample.FrontRight, sample.FrontCenter)
	out := mix(from, remixSettings(sample.Stereo()), [][]float32{{0}, {0}, {1}})
	want := amplitude(halfPower)
	if out[0][0] != want || out[1][0] != want {
		t.Fatalf("centre folded to %v and %v, want %v each", out[0][0], out[1][0], want)
	}
}

// An absent level is a request to drop the channel rather than a missing
// setting, which is the only way to say that low frequency should not be
// folded into the front pair.
func TestRemixDropsAChannelWithNoLevel(t *testing.T) {
	from := sample.Positions(sample.FrontLeft, sample.FrontRight, sample.LowFrequency)
	out := mix(from, remixSettings(sample.Stereo()), [][]float32{{0.5}, {0.5}, {1}})
	if out[0][0] != 0.5 || out[1][0] != 0.5 {
		t.Fatalf("a dropped channel reached the output: %v", out)
	}
}

// Spreading one channel over more of them cannot lose anything; folding
// several into fewer can, and the Plan has to say which happened.
func TestRemixReportsWhetherTheFoldCanBeUndone(t *testing.T) {
	if got := remixLoss(sample.Mono(), sample.Stereo()); got != 0 {
		t.Fatalf("widening reported loss %v", got)
	}
	if got := remixLoss(sample.Positions(sample.FrontLeft, sample.FrontRight, sample.FrontCenter), sample.Stereo()); got == 0 {
		t.Fatal("folding three channels into two reported no loss")
	}
}

// Folding adds channels together, so a sum can leave the range the samples
// were in. Normalizing scales the whole frame rather than clipping the part
// that overshot.
func TestRemixNormalizesAFoldThatOvershoots(t *testing.T) {
	from := sample.Positions(sample.FrontLeft, sample.FrontRight, sample.FrontCenter)
	settings := remixSettings(sample.Stereo())
	settings.Center = config.Some[config.DecibelValue](0)
	settings.Normalize = true
	out := mix(from, settings, [][]float32{{1}, {1}, {1}})
	if out[0][0] != 1 || out[1][0] != 1 {
		t.Fatalf("normalized fold = %v, want full scale on both", out)
	}
}
