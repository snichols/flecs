package docs_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// TestDocLinks verifies that every internal link ([text](file.md) or
// [text](file.md#anchor)) in the docs/ directory resolves: the target file
// must exist and, when an anchor is specified, the heading must be present.
func TestDocLinks(t *testing.T) {
	docsDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}

	mdFiles, err := filepath.Glob(filepath.Join(docsDir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(mdFiles) == 0 {
		t.Fatal("no .md files found in docs/")
	}

	// Pre-build heading anchor sets for each .md file (lazy).
	anchorCache := map[string]map[string]bool{}
	getAnchors := func(path string) map[string]bool {
		if anchors, ok := anchorCache[path]; ok {
			return anchors
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		anchors := extractAnchors(string(data))
		anchorCache[path] = anchors
		return anchors
	}

	// Only match links whose href looks like a relative .md file (with optional
	// anchor), not function-call argument lists.
	linkRe := regexp.MustCompile(`\[([^\]]*)\]\(([A-Za-z][A-Za-z0-9_./%-]*\.md(?:#[A-Za-z0-9._%-]*)?)\)`)

	for _, src := range mdFiles {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Errorf("cannot read %s: %v", src, err)
			continue
		}
		srcBase := filepath.Base(src)
		content := stripCodeBlocks(string(data))

		matches := linkRe.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			href := m[2]

			// Skip external or anchor-only links.
			if strings.HasPrefix(href, "http://") ||
				strings.HasPrefix(href, "https://") ||
				strings.HasPrefix(href, "#") ||
				strings.HasPrefix(href, "mailto:") {
				continue
			}

			file, anchor, _ := strings.Cut(href, "#")
			target := filepath.Join(docsDir, file)

			if _, err := os.Stat(target); os.IsNotExist(err) {
				t.Errorf("%s: broken link — target file not found: %s", srcBase, file)
				continue
			}

			if anchor == "" {
				continue
			}

			anchors := getAnchors(target)
			if anchors == nil {
				t.Errorf("%s: cannot read target %s to check anchor #%s", srcBase, file, anchor)
				continue
			}
			if !anchors[anchor] {
				t.Errorf("%s: broken anchor — #%s not found in %s", srcBase, anchor, file)
			}
		}
	}
}

// stripCodeBlocks removes fenced code blocks (```...```) and inline code
// (`...`) from Markdown source so link patterns inside them are ignored.
func stripCodeBlocks(md string) string {
	// Remove fenced code blocks first.
	fenced := regexp.MustCompile("(?s)```[^`]*```")
	md = fenced.ReplaceAllLiteralString(md, "")
	// Remove inline code spans.
	inline := regexp.MustCompile("`[^`\n]+`")
	md = inline.ReplaceAllLiteralString(md, "")
	return md
}

// explicitIDRe matches the Pandoc/kramdown explicit-ID attribute {#id} at the
// end of a heading line, e.g. "## Merge hooks {#merge-hooks}".
var explicitIDRe = regexp.MustCompile(`\{#([A-Za-z0-9_-]+)\}\s*$`)

// extractAnchors returns the GitHub-style anchor IDs for every ATX heading
// (# … through ###### …) in the Markdown source.  It also recognises explicit
// Pandoc-style {#id} attributes, which GitHub renders as the canonical anchor.
func extractAnchors(md string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimLeft(line, "#")
		if len(trimmed) == len(line) {
			continue // not a heading line
		}
		if len(trimmed) > 0 && trimmed[0] != ' ' {
			continue // '#' must be followed by space
		}
		heading := strings.TrimSpace(trimmed)

		// Prefer an explicit {#id} attribute when present.
		if m := explicitIDRe.FindStringSubmatch(heading); m != nil {
			out[m[1]] = true
			// Also add the auto-generated anchor (minus the {#id} suffix).
			strippedHeading := strings.TrimSpace(explicitIDRe.ReplaceAllLiteralString(heading, ""))
			out[headingToAnchor(strippedHeading)] = true
			continue
		}

		out[headingToAnchor(heading)] = true
	}
	return out
}

// headingToAnchor converts a Markdown heading string to the GitHub-style
// anchor: lowercase, spaces and hyphens become hyphens, non-alphanumeric/
// hyphen/underscore characters are dropped.
func headingToAnchor(h string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(h) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == ' ' || r == '-':
			b.WriteByte('-')
		case r == '_':
			b.WriteByte('_')
			// other punctuation dropped
		}
	}
	return strings.TrimRight(b.String(), "-")
}
