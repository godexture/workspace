package cliflag

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

type testMode string

type testConfig struct {
	Enabled bool          `cli:"enabled" help:"Enable processing"`
	Limit   int           `cli:"limit" help:"Processing limit"`
	Delay   time.Duration `cli:"delay" help:"Processing delay"`
	Mode    testMode      `cli:"mode" help:"Processing mode"`
	Tags    []string      `cli:"tag" help:"Processing tags"`
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
	prototype := &testConfig{Enabled: true, Limit: 5, Delay: time.Second, Mode: "safe"}
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
	if !actual.Enabled || actual.Limit != 8 || actual.Delay != time.Second || actual.Mode != "safe" || strings.Join(actual.Tags, ",") != "one,two" {
		t.Fatalf("Apply() = %#v", actual)
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
	_, err := BindStruct(flags, "", &testConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if usage := flags.Lookup("enabled").Usage; !strings.Contains(usage, "default: false") {
		t.Fatalf("enabled usage = %q", usage)
	}
	if usage := flags.Lookup("limit").Usage; !strings.Contains(usage, "default: 0") {
		t.Fatalf("limit usage = %q", usage)
	}
}

func TestBindingOwnsEveryDefaultDisplay(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	_, err := BindStruct(flags, "", &testConfig{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	flag := flags.Lookup("limit")
	if flag.DefValue != "" {
		t.Fatalf("DefValue = %q, want empty", flag.DefValue)
	}
	if !strings.Contains(flag.Usage, "default: 5") {
		t.Fatalf("limit usage = %q", flag.Usage)
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
