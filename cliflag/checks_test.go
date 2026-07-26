package cliflag

import (
	"math"
	"reflect"
	"testing"

	"github.com/spf13/pflag"
)

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
	}{{"outside scope", "fast", false}, {"inside scope", "safe", true}} {
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
			if err := CheckFields(config.value); (err != nil) != config.wantErr {
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
