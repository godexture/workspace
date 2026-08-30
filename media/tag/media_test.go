package tag

import "testing"

func TestIsImageMediaType(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: "image/png", want: true},
		{value: "IMAGE/vnd.example+json", want: true},
		{value: "image/x~test", want: true},
		{value: "application/pdf"},
		{value: "image/"},
		{value: "image/png;foo"},
		{value: "image/png foo"},
		{value: "image//png"},
		{value: "image/p\x00ng"},
	} {
		if got := IsImageMediaType(test.value); got != test.want {
			t.Errorf("IsImageMediaType(%q) = %v, want %v", test.value, got, test.want)
		}
	}
}
