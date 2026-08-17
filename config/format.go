package config

import (
	"fmt"
	"io"
	"strconv"
)

func writeSafeFormat(state fmt.State, verb rune, value string) {
	if verb == 'q' {
		value = strconv.Quote(value)
	}
	_, _ = io.WriteString(state, value)
}
