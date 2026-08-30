package tag

import "strings"

// IsImageMediaType reports whether value is an RFC 2046 image media type.
// Metadata encoding plugins use it before exposing artwork semantically.
func IsImageMediaType(value string) bool {
	major, minor, found := strings.Cut(value, "/")
	return found && strings.EqualFold(major, "image") && mediaToken(major) && mediaToken(minor)
}

func mediaToken(value string) bool {
	if value == "" {
		return false
	}
	for _, byteValue := range []byte(value) {
		if (byteValue >= 'a' && byteValue <= 'z') || (byteValue >= 'A' && byteValue <= 'Z') || (byteValue >= '0' && byteValue <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(byteValue)) {
			continue
		}
		return false
	}
	return true
}
