// Package strutil provides small string helpers.
package strutil

import "strings"

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
