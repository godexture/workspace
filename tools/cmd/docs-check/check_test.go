package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGithubSlugKeepsUnicodeAndNumbers(t *testing.T) {
	if got := githubSlug("M3 完了条件: `audio`!"); got != "m3-完了条件-audio" {
		t.Fatalf("slug = %q", got)
	}
}

func TestCheckReportsMissingFileAndAnchor(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.md"), []byte("[missing](no.md)\n[anchor](target.md#absent)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "target.md"), []byte("# Present\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	issues, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 {
		t.Fatalf("issues = %v", issues)
	}
	joined := issues[0].Error() + "\n" + issues[1].Error()
	if !strings.Contains(joined, "index.md:1") || !strings.Contains(joined, "index.md:2") {
		t.Fatalf("line information missing: %s", joined)
	}
	if !strings.Contains(joined, "does not exist") || !strings.Contains(joined, "does not exist in") {
		t.Fatalf("reasons missing: %s", joined)
	}
}

func TestCheckAcceptsDuplicateHeadingAnchor(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.md"), []byte("[second](target.md#same-1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "target.md"), []byte("# Same\n# Same\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	issues, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %v", issues)
	}
}
