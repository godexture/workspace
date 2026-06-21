package id3text

import "strings"

func TrimString(value string) string {
	return strings.Trim(value, "\x00 \t\r\n")
}

func Latin1ToUTF8(payload []byte) string {
	runes := make([]rune, len(payload))
	for i, b := range payload {
		runes[i] = rune(b)
	}
	return string(runes)
}
