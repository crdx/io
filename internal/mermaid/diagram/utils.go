package diagram

import (
	"regexp"
	"strings"
)

// RemoveComments removes Mermaid comment lines from input. This function is shared between graph
// and sequence diagram parsers. It handles both full-line comments (%% at start) and inline
// comments (%% after content).
func RemoveComments(lines []string) []string {
	cleaned := make([]string, 0, len(lines))

	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "%%") {
			continue
		}

		if index := strings.Index(line, "%%"); index != -1 {
			line = strings.TrimSpace(line[:index])
		}

		if len(strings.TrimSpace(line)) > 0 {
			cleaned = append(cleaned, line)
		}
	}

	return cleaned
}

// SplitLines splits input on both actual newlines and escaped newlines (for curl compatibility).
func SplitLines(input string) []string {
	newlinePattern := regexp.MustCompile(`\n|\\n`)
	return newlinePattern.Split(input, -1)
}
