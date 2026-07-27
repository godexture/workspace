package play

import "testing"

func TestKeyDecoder(t *testing.T) {
	tests := []struct {
		input string
		want  action
	}{
		{input: " ", want: actionToggle},
		{input: "q", want: actionQuit},
		{input: "\x1b[A", want: actionUp},
		{input: "\x1b[B", want: actionDown},
		{input: "\xe0H", want: actionUp},
		{input: "\xe0P", want: actionDown},
		{input: "\x1bq", want: actionQuit},
		{input: "xq", want: actionQuit},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			decoder := keyDecoder{}
			for _, key := range []byte(test.input) {
				action, ok := decoder.Push(key)
				if ok {
					if action != test.want {
						t.Fatalf("Push(%q) action = %v, want %v", key, action, test.want)
					}
					return
				}
			}
			t.Fatal("decoder emitted no action")
		})
	}
}
