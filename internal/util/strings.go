package util

import "strings"

// JoinNonEmpty joins non-empty parts with spaces.
func JoinNonEmpty(parts ...string) string {
	nonEmptyParts := make([]string, 0, len(parts))

	for _, part := range parts {
		if part != "" {
			nonEmptyParts = append(nonEmptyParts, part)
		}
	}

	return strings.Join(nonEmptyParts, " ")
}
