package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestListRoleIncludesDescriptions(t *testing.T) {
	command := newListCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"encoders"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	actual := output.String()
	for _, expected := range []string{"encoders:", "flac", "FLAC encoder", "pcm", "LPCM/G.711/ADPCM encoder"} {
		if !strings.Contains(actual, expected) {
			t.Fatalf("list output does not contain %q:\n%s", expected, actual)
		}
	}
}

func TestListRejectsUnknownRole(t *testing.T) {
	command := newListCommand()
	command.SetArgs([]string{"encoder"})
	if err := command.Execute(); err == nil {
		t.Fatal("list accepted an unknown role")
	}
}

func TestListAliases(t *testing.T) {
	for _, test := range []struct {
		role     string
		expected []string
	}{
		{role: "formats", expected: []string{"muxers:", "demuxers:"}},
		{role: "codecs", expected: []string{"encoders:", "decoders:"}},
	} {
		t.Run(test.role, func(t *testing.T) {
			command := newListCommand()
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetArgs([]string{test.role})
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			for _, expected := range test.expected {
				if !strings.Contains(output.String(), expected) {
					t.Fatalf("list %s output does not contain %q:\n%s", test.role, expected, output.String())
				}
			}
		})
	}
}
