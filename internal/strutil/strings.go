package strutil

import "strings"

// FirstLine is text cut down to its first line.
func FirstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}

// Lines splits text into lines without adding a line after a trailing newline.
func Lines(text string) []string {
	if text == "" {
		return nil
	}

	lines := strings.Split(text, "\n")
	if strings.HasSuffix(text, "\n") {
		lines = lines[:len(lines)-1]
	}

	return lines
}
