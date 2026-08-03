package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var (
	markdownLink = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	heading      = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.+?)\s*$`)
	htmlTag      = regexp.MustCompile(`<[^>]*>`)
)

// Issue is one broken local Markdown link.
type Issue struct {
	File   string
	Line   int
	Link   string
	Reason string
}

func (i Issue) Error() string {
	return fmt.Sprintf("%s:%d: link %q: %s", i.File, i.Line, i.Link, i.Reason)
}

// Check validates local links and heading anchors below root. Absolute URLs
// and other URI schemes are intentionally outside this gate.
func Check(root string) ([]Issue, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("docs root %q: %w", root, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("docs root %q is not a directory", root)
	}

	var files []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(files)

	contents := make(map[string][]byte, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		contents[file] = data
	}

	var issues []Issue
	for _, file := range files {
		lines := strings.Split(string(contents[file]), "\n")
		inFence := false
		for lineNumber, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			matches := markdownLink.FindAllStringSubmatch(maskInlineCode(line), -1)
			for _, match := range matches {
				destination := strings.TrimSpace(match[1])
				if destination == "" {
					issues = append(issues, Issue{File: displayPath(file, root), Line: lineNumber + 1, Link: destination, Reason: "empty destination"})
					continue
				}
				if strings.HasPrefix(destination, "<") && strings.Contains(destination, ">") {
					destination = destination[1:strings.IndexByte(destination, '>')]
				}
				parsed, err := url.Parse(destination)
				if err != nil {
					issues = append(issues, Issue{File: displayPath(file, root), Line: lineNumber + 1, Link: match[1], Reason: "invalid URI: " + err.Error()})
					continue
				}
				if parsed.IsAbs() || parsed.Scheme != "" || strings.HasPrefix(destination, "//") {
					continue
				}

				target := file
				if parsed.Path != "" {
					pathPart, err := url.PathUnescape(parsed.Path)
					if err != nil {
						issues = append(issues, Issue{File: displayPath(file, root), Line: lineNumber + 1, Link: match[1], Reason: "invalid path escape: " + err.Error()})
						continue
					}
					target = filepath.Clean(filepath.Join(filepath.Dir(file), filepath.FromSlash(pathPart)))
				}
				info, statErr := os.Stat(target)
				if statErr != nil {
					issues = append(issues, Issue{File: displayPath(file, root), Line: lineNumber + 1, Link: match[1], Reason: fmt.Sprintf("target %q does not exist", displayPath(target, root))})
					continue
				}
				if info.IsDir() && parsed.Fragment == "" {
					continue
				}
				data, ok := contents[target]
				if !ok {
					data, err = os.ReadFile(target)
					if err != nil {
						issues = append(issues, Issue{File: displayPath(file, root), Line: lineNumber + 1, Link: match[1], Reason: fmt.Sprintf("target %q cannot be read: %v", displayPath(target, root), err)})
						continue
					}
					contents[target] = data
				}
				if parsed.Fragment != "" && !hasAnchor(data, parsed.Fragment) {
					issues = append(issues, Issue{File: displayPath(file, root), Line: lineNumber + 1, Link: match[1], Reason: fmt.Sprintf("anchor %q does not exist in %q", parsed.Fragment, displayPath(target, root))})
				}
			}
		}
	}
	return issues, nil
}

func maskInlineCode(line string) string {
	var builder strings.Builder
	inCode := false
	for _, r := range line {
		if r == '`' {
			inCode = !inCode
			builder.WriteByte(' ')
			continue
		}
		if inCode {
			if r == '\t' {
				builder.WriteByte('\t')
			} else {
				builder.WriteByte(' ')
			}
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func displayPath(path, root string) string {
	if relative, err := filepath.Rel(filepath.Dir(root), path); err == nil {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(path)
}

func hasAnchor(data []byte, fragment string) bool {
	anchors := map[string]struct{}{}
	counts := map[string]int{}
	for _, line := range strings.Split(string(data), "\n") {
		match := heading.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		slug := githubSlug(match[1])
		if slug == "" {
			continue
		}
		index := counts[slug]
		counts[slug] = index + 1
		if index != 0 {
			slug = fmt.Sprintf("%s-%d", slug, index)
		}
		anchors[slug] = struct{}{}
	}
	_, ok := anchors[strings.TrimPrefix(fragment, "#")]
	return ok
}

func githubSlug(value string) string {
	value = htmlTag.ReplaceAllString(value, "")
	value = strings.ReplaceAll(value, "`", "")
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastHyphen := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-':
			builder.WriteRune(r)
			lastHyphen = r == '-'
		case unicode.IsSpace(r):
			if builder.Len() != 0 && !lastHyphen {
				builder.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}
