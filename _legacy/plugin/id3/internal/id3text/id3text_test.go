package id3text

import (
	"testing"
)

func TestTrimString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"  hello  ", "hello"},
		{"\x00hello\x00", "hello"},
		{"\r\n\t hello \t\r\n", "hello"},
		{"hello world", "hello world"},
	}

	for _, test := range tests {
		actual := TrimString(test.input)
		if actual != test.expected {
			t.Errorf("TrimString(%q) = %q, want %q", test.input, actual, test.expected)
		}
	}
}

func TestLatin1ToUTF8(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    []byte
		expected string
	}{
		{[]byte("hello"), "hello"},
		{[]byte{0xE9}, "é"}, // Latin1 'é' is 0xE9
		{[]byte{0xA9}, "©"}, // Latin1 '©' is 0xA9
		{[]byte{0xE4, 0xF6, 0xFC}, "äöü"},
	}

	for _, test := range tests {
		actual := Latin1ToUTF8(test.input)
		if actual != test.expected {
			t.Errorf("Latin1ToUTF8(%v) = %q, want %q", test.input, actual, test.expected)
		}
	}
}
