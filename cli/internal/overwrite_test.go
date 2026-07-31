package cli

import (
	"testing"
)

func TestOverwriteAnswer(t *testing.T) {
	tests := []struct {
		name      string
		answer    string
		confirmed bool
	}{
		{name: "yes", answer: "y", confirmed: true},
		{name: "uppercase yes", answer: "Y", confirmed: true},
		{name: "surrounding whitespace", answer: " y ", confirmed: true},
		{name: "no", answer: "n"},
		{name: "default", answer: ""},
		{name: "invalid", answer: "xy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if confirmed := overwriteConfirmed(test.answer); confirmed != test.confirmed {
				t.Fatalf("confirmed = %v, want %v", confirmed, test.confirmed)
			}
		})
	}
}
