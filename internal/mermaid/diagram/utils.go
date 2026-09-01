package diagram

import (
	"regexp"
	"strings"
)

func RemoveComments(lines []string) []string {
	cleanedLines := make([]string, 0, len(lines))

	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "%%") {
			continue
		}

		if index := strings.Index(line, "%%"); index != -1 {
			line = strings.TrimSpace(line[:index])
		}

		if len(strings.TrimSpace(line)) > 0 {
			cleanedLines = append(cleanedLines, line)
		}
	}

	return cleanedLines
}

func SplitLines(input string) []string {
	newlinePattern := regexp.MustCompile(`\n|\\n`)
	return newlinePattern.Split(input, -1)
}
