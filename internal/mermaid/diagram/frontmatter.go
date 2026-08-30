package diagram

import "strings"

// StripFrontmatter removes a leading YAML frontmatter block (delimited by `---` lines) from a
// mermaid document, returning the remaining input and the frontmatter's title, if one was set.
//
// Mermaid uses frontmatter for a diagram title and theme/config overrides (colours, CSS). The
// config has no meaning in ASCII output, so it is discarded; the title is surfaced so callers can
// print it above the diagram, as mermaid does.
//
// Matching mermaid's own frontmatter semantics (frontmatter.spec.ts):
// frontmatter is only recognised at the start of the document, the closing delimiter must sit at
// the same indentation as the opening one, and an unclosed block is not frontmatter at all — the
// input is returned untouched for the diagram parser to deal with.
func StripFrontmatter(input string) (string, string) {
	lines := strings.Split(input, "\n")
	title := ""

	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) || !isDelimiter(lines[start]) {
		return input, ""
	}
	indent := indentOf(lines[start])

	for i := start + 1; i < len(lines); i++ {
		if isDelimiter(lines[i]) && indentOf(lines[i]) == indent {
			return strings.Join(lines[i+1:], "\n"), title
		}
		trimmed := strings.TrimRight(lines[i], " \t\r")
		if rest, ok := strings.CutPrefix(trimmed, indent+"title:"); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t') {
			rest = strings.TrimSpace(rest)
			if !strings.HasPrefix(rest, `"`) && !strings.HasPrefix(rest, `'`) {
				if index := strings.Index(rest, " #"); index != -1 {
					rest = strings.TrimSpace(rest[:index])
				}
			}
			title = strings.Trim(rest, `"'`)
		}
	}
	return input, ""
}

func indentOf(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

func isDelimiter(line string) bool {
	return strings.TrimSpace(line) == "---"
}
