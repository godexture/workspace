package access

import (
	"fmt"
	"strings"
	"testing"
)

func TestReferenceDisplayRedactsEveryTargetForm(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		scheme string
		leaks  []string
	}{
		{name: "opaque", input: "vault:super-secret", scheme: "vault", leaks: []string{"super-secret"}},
		{name: "hierarchical", input: "https://user:password@example.com/private/audio.wav?token=signed#fragment", scheme: "https", leaks: []string{"user", "password", "example.com", "private", "token", "signed", "fragment"}},
		{name: "local path", input: "file:///private/local/secret.wav", scheme: "file", leaks: []string{"private", "local", "secret.wav"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			reference, err := Parse(testCase.input)
			if err != nil {
				t.Fatal(err)
			}
			want := testCase.scheme + ":" + redactedReferenceTarget
			if reference.Display() != want {
				t.Fatalf("display = %q, want %q", reference.Display(), want)
			}
			for _, leak := range testCase.leaks {
				for _, formatted := range []string{
					fmt.Sprintf("%v", reference),
					fmt.Sprintf("%+v", reference),
					fmt.Sprintf("%#v", reference),
					fmt.Sprintf("%q", reference),
					fmt.Sprintf("%+q", reference),
					fmt.Sprintf("%s", reference),
					fmt.Sprintf("%x", reference),
				} {
					if strings.Contains(formatted, leak) {
						t.Fatalf("public reference representation leaked %q: %q", leak, formatted)
					}
				}
			}
			if got := fmt.Sprintf("%#v", reference); got != want {
				t.Fatalf("formatted display = %q, want %q", got, want)
			}
		})
	}
}

func TestReferenceDisplayDoesNotBecomeAnIdentity(t *testing.T) {
	first, err := Parse("file:///private/one.wav")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Parse("file:///private/two.wav")
	if err != nil {
		t.Fatal(err)
	}
	if first.Display() != second.Display() {
		t.Fatalf("display labels differ by private target: %q, %q", first.Display(), second.Display())
	}
	if first.Fingerprint() == second.Fingerprint() {
		t.Fatal("private canonical targets received the same fingerprint")
	}
	if first.Display() == first.Canonical() || second.Display() == second.Canonical() {
		t.Fatal("display unexpectedly exposed the canonical target")
	}
}
