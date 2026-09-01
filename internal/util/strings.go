package util

import "strings"

func JoinNonEmpty(parts ...string) string {
	nonEmptyParts := make([]string, 0, len(parts))

	for _, part := range parts {
		if part != "" {
			nonEmptyParts = append(nonEmptyParts, part)
		}
	}

	return strings.Join(nonEmptyParts, " ")
}
