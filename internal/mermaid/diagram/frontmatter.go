package diagram

import "strings"

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
		trimmedLine := strings.TrimRight(lines[i], " \t\r")
		if rest, ok := strings.CutPrefix(trimmedLine, indent+"title:"); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t') {
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
