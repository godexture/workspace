package workspace

import "fmt"

func SetupCLI(args []string, goCommand, workPath string, flagNeedsValue func(string) bool) (goWork string, passthroughFlags, pkgPattern []string, err error) {
	args = EnsurePackagePattern(args, flagNeedsValue)
	passthroughFlags, pkgPattern = SplitPackagePattern(args, flagNeedsValue)

	goWork, err = ResolveGoWork(goCommand, workPath)
	if err != nil {
		return "", nil, nil, err
	}

	modules, err := WorkspaceModules(goCommand, goWork)
	if err != nil {
		return "", nil, nil, err
	}
	if len(modules) == 0 {
		return "", nil, nil, fmt.Errorf("no modules found in %s", goWork)
	}
	pkgPattern = WorkspacePackagePatterns(modules, pkgPattern)

	return goWork, passthroughFlags, pkgPattern, nil
}
