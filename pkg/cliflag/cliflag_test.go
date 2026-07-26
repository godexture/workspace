package cliflag

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/godexture/core/domain/media"
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
	if !actual.Enabled || actual.Limit != 8 || actual.Delay != time.Second || actual.Mode != "safe" || strings.Join(actual.Tags, ",") != "one,two" {
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
	_, err := BindStruct(flags, "", &testConfig{})
	if err != nil {
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
	_, err := BindStruct(flags, "", &testConfig{Limit: 5})
	if err != nil {
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

func TestFieldsForRejectsInvalidDependenciesAndChecks(t *testing.T) {
	tests := []struct {
		name   string
		typeOf reflect.Type
	}{
		{"malformed dependency", reflect.TypeFor[struct {
			Value int `name:"value" depends-on:"mode"`
		}]()},
		{"empty dependency field", reflect.TypeFor[struct {
			Value int `name:"value" depends-on:"=safe"`
		}]()},
		{"empty dependency values", reflect.TypeFor[struct {
			Value int `name:"value" depends-on:"mode="`
		}]()},
		{"self dependency", reflect.TypeFor[struct {
			Value int `name:"value" depends-on:"value=safe"`
		}]()},
		{"unknown dependency", reflect.TypeFor[struct {
			Value int `name:"value" depends-on:"missing=safe"`
		}]()},
		{"slice dependency", reflect.TypeFor[struct {
			Tags  []string `name:"tag"`
			Value int      `name:"value" depends-on:"tag=safe"`
		}]()},
		{"check on string", reflect.TypeFor[struct {
			Value string `name:"value" check:"positive"`
		}]()},
		{"unknown check", reflect.TypeFor[struct {
			Value int `name:"value" check:"bogus"`
		}]()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := fieldsFor(test.typeOf); err == nil {
				t.Fatal("fieldsFor() succeeded")
			}
		})
	}
}

func TestValidateChecksHonorsDependenciesAcrossDecodePaths(t *testing.T) {
	for _, mode := range []struct {
		name    string
		mode    testMode
		wantErr bool
	}{
		{"outside scope", "fast", false},
		{"inside scope", "safe", true},
	} {
		t.Run(mode.name+" decode", func(t *testing.T) {
			config := testConfig{Mode: mode.mode, Dependent: -1}
			err := DecodeStruct(&config, nil)
			if (err != nil) != mode.wantErr {
				t.Fatalf("DecodeStruct() error = %v, wantErr %t", err, mode.wantErr)
			}
		})
		t.Run(mode.name+" binding", func(t *testing.T) {
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			config := testConfig{Mode: mode.mode, Dependent: -1}
			binding, err := BindStruct(flags, "", &config)
			if err != nil {
				t.Fatal(err)
			}
			err = binding.Apply(&config)
			if (err != nil) != mode.wantErr {
				t.Fatalf("Apply() error = %v, wantErr %t", err, mode.wantErr)
			}
		})
	}
}

func TestCheckFieldsHonorsDependenciesAndNumericChecks(t *testing.T) {
	for _, config := range []struct {
		name    string
		value   any
		wantErr bool
	}{
		{"outside dependency", &testConfig{Mode: "fast", Dependent: -1}, false},
		{"inside dependency", &testConfig{Mode: "safe", Dependent: -1}, true},
		{"nonnegative", &struct {
			Value int `name:"value" check:"nonnegative"`
		}{Value: -1}, true},
		{"finite", &struct {
			Value float64 `name:"value" check:"finite"`
		}{Value: math.Inf(1)}, true},
	} {
		t.Run(config.name, func(t *testing.T) {
			err := CheckFields(config.value)
			if (err != nil) != config.wantErr {
				t.Fatalf("CheckFields() error = %v, wantErr %t", err, config.wantErr)
			}
		})
	}
}

func TestCheckFieldsResolvesLaterDependencyByStructIndex(t *testing.T) {
	type config struct {
		skipped   int
		Dependent int    `name:"dependent" depends-on:"mode=safe" check:"positive"`
		Mode      string `name:"mode"`
	}
	if err := CheckFields(&config{Mode: "safe", Dependent: -1}); err == nil {
		t.Fatal("CheckFields() succeeded")
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
