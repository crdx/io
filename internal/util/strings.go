package util

import "strings"

const datedSnapshotLength = len("20060102")

func IsDatedSnapshot(segment string) bool {
	if len(segment) != datedSnapshotLength {
		return false
	}

	for _, character := range segment {
		if character < '0' || character > '9' {
			return false
		}
	}

	return true
}

func JoinNonEmpty(parts ...string) string {
	nonEmptyParts := make([]string, 0, len(parts))

	for _, part := range parts {
		if part != "" {
			nonEmptyParts = append(nonEmptyParts, part)
		}
	}

	return strings.Join(nonEmptyParts, " ")
}
