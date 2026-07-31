package registry

type Defaulter interface {
	ApplyDefaults()
}

type Validator interface {
	Validate() error
}

func isManifestName(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	previousDash := false
	for _, char := range name {
		isLower := char >= 'a' && char <= 'z'
		isDigit := char >= '0' && char <= '9'
		if char == '-' {
			if previousDash {
				return false
			}
			previousDash = true
			continue
		}
		if !isLower && !isDigit {
			return false
		}
		previousDash = false
	}
	return !previousDash
}

func hasDuplicates[T comparable](values []T) bool {
	seen := make(map[T]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}
