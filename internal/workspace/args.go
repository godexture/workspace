package workspace

import "strings"

func SplitArgs(args []string) ([]string, []string) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

func EnsurePackagePattern(args []string, flagNeedsValue func(string) bool) []string {
	if len(args) == 0 {
		return []string{"./..."}
	}

	start, argsIndex := findPatternBounds(args, flagNeedsValue)
	if start < argsIndex {
		return args
	}

	withPattern := make([]string, 0, len(args)+1)
	withPattern = append(withPattern, args[:argsIndex]...)
	withPattern = append(withPattern, "./...")
	withPattern = append(withPattern, args[argsIndex:]...)
	return withPattern
}

func argsSeparatorIndex(args []string) int {
	for i, arg := range args {
		if arg == "-args" {
			return i
		}
	}
	return len(args)
}

func findPatternBounds(args []string, flagNeedsValue func(string) bool) (start, argsIndex int) {
	argsIndex = argsSeparatorIndex(args)
	i := 0
	for i < argsIndex {
		arg := args[i]
		if arg == "" {
			i++
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			break
		}
		if flagNeedsValue != nil && flagNeedsValue(arg) && !strings.Contains(arg, "=") {
			i++
		}
		i++
	}
	return i, argsIndex
}

func SplitPackagePattern(args []string, flagNeedsValue func(string) bool) (flags, pattern []string) {
	start, argsIndex := findPatternBounds(args, flagNeedsValue)
	flags = append(flags, args[:start]...)
	pattern = append(pattern, args[start:argsIndex]...)
	flags = append(flags, args[argsIndex:]...)
	return flags, pattern
}

func AppendPackageArgs(args, packages []string) []string {
	separator := argsSeparatorIndex(args)
	if separator == len(args) {
		return append(args, packages...)
	}
	result := make([]string, 0, len(args)+len(packages))
	result = append(result, args[:separator]...)
	result = append(result, packages...)
	result = append(result, args[separator:]...)
	return result
}
