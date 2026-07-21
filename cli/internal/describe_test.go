package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestDescribeCodec(t *testing.T) {
	command := newDescribeCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"codec", "flac"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	actual := output.String()
	for _, expected := range []string{
		"--codec flac[:key=value,...]",
		"block-size",
		"[default: 4096]",
		"choices: independent, adaptive, exhaustive",
	} {
		if !strings.Contains(actual, expected) {
			t.Fatalf("describe output does not contain %q:\n%s", expected, actual)
		}
	}
}

func TestDescribeFormat(t *testing.T) {
	command := newDescribeCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"format", "wav"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	actual := output.String()
	if !strings.Contains(actual, "--format wav[:key=value,...]") || !strings.Contains(actual, "[default: false]") {
		t.Fatalf("describe output = %s", actual)
	}
}
