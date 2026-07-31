package cliflag

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/godexture/godec/core/domain/media"
	"github.com/spf13/pflag"
)

type testMode string

type testConfig struct {
	Enabled   bool          `name:"enabled" help:"Enable processing"`
	Limit     int           `name:"limit" help:"Processing limit"`
	Delay     time.Duration `name:"delay" help:"Processing delay"`
	Mode      testMode      `name:"mode" help:"Processing mode"`
	Tags      []string      `name:"tag" help:"Processing tags"`
	Dependent int           `name:"dependent" depends-on:"mode=safe" check:"positive"`
}

type layoutConfig struct {
	Layout media.ChannelLayout `name:"layout"`
}

func (testConfig) FieldChoices(field string) []string {
	if field == "Mode" {
		return []string{"fast", "safe"}
	}
	return nil
}

func (c testConfig) Validate() error {
	if c.Limit < 0 {
		return errInvalidLimit
	}
	return nil
}

var errInvalidLimit = testError("limit must not be negative")

type testError string

func (e testError) Error() string { return string(e) }

func TestBindingAppliesOnlyChangedFlags(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	prototype := &testConfig{Enabled: true, Limit: 5, Delay: time.Second, Mode: "safe", Dependent: 1}
	binding, err := BindStruct(flags, "encoder.flac", prototype)
	if err != nil {
		t.Fatal(err)
	}
	if err := flags.Parse([]string{"--encoder.flac.limit=8", "--encoder.flac.tag=one", "--encoder.flac.tag=two"}); err != nil {
		t.Fatal(err)
	}
	actual := *prototype
	if err := binding.Apply(&actual); err != nil {
		t.Fatal(err)
	}
	if !actual.Enabled || actual.Limit != 8 || actual.Delay != time.Second || actual.Mode != "safe" || actual.Dependent != 1 || strings.Join(actual.Tags, ",") != "one,two" {
		t.Fatalf("Apply() = %#v", actual)
	}
}

func TestDecodeStructSupportsTextUnmarshalerFields(t *testing.T) {
	var config layoutConfig
	if err := DecodeStruct(&config, map[string]string{"layout": "stereo"}); err != nil {
		t.Fatal(err)
	}
	if config.Layout != media.LayoutStereo2_0 {
		t.Fatalf("layout = %s, want %s", config.Layout, media.LayoutStereo2_0)
	}
}

func TestBindingRejectsInvalidChoiceWithoutMutatingTarget(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	binding, err := BindStruct(flags, "", &testConfig{Mode: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	if err := flags.Parse([]string{"--mode=invalid"}); err != nil {
		t.Fatal(err)
	}
	actual := testConfig{Mode: "safe"}
	if err := binding.Apply(&actual); err == nil {
		t.Fatal("Apply() succeeded")
	}
	if actual.Mode != "safe" {
		t.Fatalf("target mutated: %#v", actual)
	}
}

func TestBindingShowsZeroValueDefaults(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	if _, err := BindStruct(flags, "", &testConfig{}); err != nil {
		t.Fatal(err)
	}
	if usage := flags.Lookup("enabled").Usage; !strings.Contains(usage, "[false]") {
		t.Fatalf("enabled usage = %q", usage)
	}
	if usage := flags.Lookup("limit").Usage; !strings.Contains(usage, "[0]") {
		t.Fatalf("limit usage = %q", usage)
	}
}

func TestBindingOwnsEveryDefaultDisplay(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	if _, err := BindStruct(flags, "", &testConfig{Limit: 5}); err != nil {
		t.Fatal(err)
	}
	flag := flags.Lookup("limit")
	if flag.DefValue != "0" {
		t.Fatalf("DefValue = %q, want 0", flag.DefValue)
	}
	if !strings.Contains(flag.Usage, "[5]") {
		t.Fatalf("limit usage = %q", flag.Usage)
	}
}

func TestDescribeStruct(t *testing.T) {
	descriptions, err := DescribeStruct(&testConfig{Enabled: true, Limit: 5, Mode: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptions) != 6 {
		t.Fatalf("len(DescribeStruct()) = %d", len(descriptions))
	}
	mode := descriptions[3]
	if mode.Name != "mode" || mode.Type != "enum" || mode.Default != "safe" || !slices.Equal(mode.Choices, []string{"fast", "safe"}) {
		t.Fatalf("mode description = %#v", mode)
	}
	dependent := descriptions[5]
	if dependent.DependsOn == nil || dependent.DependsOn.Field != "mode" || !slices.Equal(dependent.DependsOn.Values, []string{"safe"}) {
		t.Fatalf("dependent description = %#v", dependent)
	}
}

func TestDecodeStructValidatesAndRejectsUnknownFields(t *testing.T) {
	actual := testConfig{}
	if err := DecodeStruct(&actual, map[string]string{"limit": "3", "mode": "fast"}); err != nil {
		t.Fatal(err)
	}
	if actual.Limit != 3 || actual.Mode != "fast" {
		t.Fatalf("DecodeStruct() = %#v", actual)
	}
	if err := DecodeStruct(&actual, map[string]string{"unknown": "value"}); err == nil {
		t.Fatal("DecodeStruct() accepted an unknown field")
	}
	if err := DecodeStruct(&actual, map[string]string{"limit": "-1"}); err == nil || !strings.Contains(err.Error(), errInvalidLimit.Error()) {
		t.Fatalf("DecodeStruct() error = %v", err)
	}
}

func TestParseSpec(t *testing.T) {
	spec, err := ParseSpec(`trim:temp-dir=C:\\work\\tmp,threshold-dbfs=-40`)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != "trim" || spec.Values["temp-dir"] != `C:\work\tmp` || spec.Values["threshold-dbfs"] != "-40" {
		t.Fatalf("ParseSpec() = %#v", spec)
	}
	if _, err := ParseSpec(`trim:temp-dir=a\,b`); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSpec("trim:x=1,x=2"); err == nil {
		t.Fatal("duplicate option accepted")
	}
}
