package cliflag

import (
	"reflect"
	"testing"
)

func TestParseSpecPlain(t *testing.T) {
	t.Parallel()
	got, err := ParseSpec("equalizer:type=peaking,frequency-hz=1000")
	if err != nil {
		t.Fatal(err)
	}
	want := Spec{Name: "equalizer", Values: map[string]string{"type": "peaking", "frequency-hz": "1000"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseSpec() = %#v, want %#v", got, want)
	}
}

func TestParseSpecNameOnly(t *testing.T) {
	t.Parallel()
	got, err := ParseSpec("gain")
	if err != nil {
		t.Fatal(err)
	}
	want := Spec{Name: "gain", Values: map[string]string{}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseSpec() = %#v, want %#v", got, want)
	}
}

func TestParseSpecWithParameters(t *testing.T) {
	t.Parallel()
	got, err := ParseSpec("mixer[in=2,out=1]:normalize=true")
	if err != nil {
		t.Fatal(err)
	}
	want := Spec{
		Name:       "mixer",
		Parameters: map[string]string{"in": "2", "out": "1"},
		Values:     map[string]string{"normalize": "true"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseSpec() = %#v, want %#v", got, want)
	}
}

func TestParseSpecParametersOnlyNoColon(t *testing.T) {
	t.Parallel()
	got, err := ParseSpec("mixer[in=2,out=1]")
	if err != nil {
		t.Fatal(err)
	}
	want := Spec{
		Name:       "mixer",
		Parameters: map[string]string{"in": "2", "out": "1"},
		Values:     map[string]string{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseSpec() = %#v, want %#v", got, want)
	}
}

func TestParseSpecEmptyBrackets(t *testing.T) {
	t.Parallel()
	got, err := ParseSpec("mixer[]:normalize=true")
	if err != nil {
		t.Fatal(err)
	}
	want := Spec{
		Name:       "mixer",
		Parameters: map[string]string{},
		Values:     map[string]string{"normalize": "true"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseSpec() = %#v, want %#v", got, want)
	}
}

func TestParseSpecNoParametersHasNilParameters(t *testing.T) {
	t.Parallel()
	got, err := ParseSpec("equalizer:type=peaking")
	if err != nil {
		t.Fatal(err)
	}
	if got.Parameters != nil {
		t.Fatalf("Parameters = %#v, want nil (no bracket segment present)", got.Parameters)
	}
}

func TestParseSpecRejectsUnterminatedBracket(t *testing.T) {
	t.Parallel()
	if _, err := ParseSpec("mixer[in=2"); err == nil {
		t.Fatal("want error for unterminated '['")
	}
}

func TestParseSpecRejectsJunkAfterBracket(t *testing.T) {
	t.Parallel()
	if _, err := ParseSpec("mixer[in=2]junk"); err == nil {
		t.Fatal("want error for text after ']' that isn't ':'")
	}
}

func TestParseSpecEscapedBracketInName(t *testing.T) {
	t.Parallel()
	got, err := ParseSpec(`weird\[name:key=value`)
	if err != nil {
		t.Fatal(err)
	}
	want := Spec{Name: "weird[name", Values: map[string]string{"key": "value"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseSpec() = %#v, want %#v", got, want)
	}
}

func TestParseSpecDuplicateParameter(t *testing.T) {
	t.Parallel()
	if _, err := ParseSpec("mixer[in=2,in=3]"); err == nil {
		t.Fatal("want error for duplicate parameter key")
	}
}
