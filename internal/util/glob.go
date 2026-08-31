package util

import (
	"path"
	"strings"
)

// MatchGlob reports whether name, a slash-separated relative path, matches pattern. '*', '?' and
// '[...]' are path.Match within one segment, and '**' as a whole segment matches zero or more of
// them.
func MatchGlob(pattern string, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(patternSegments []string, nameSegments []string) bool {
	if len(patternSegments) == 0 {
		return len(nameSegments) == 0
	}

	if patternSegments[0] == "**" {
		for i := 0; i <= len(nameSegments); i++ {
			if matchSegments(patternSegments[1:], nameSegments[i:]) {
				return true
			}
		}

		return false
	}

	if len(nameSegments) == 0 {
		return false
	}

	isMatched, err := path.Match(patternSegments[0], nameSegments[0])
	if err != nil || !isMatched {
		return false
	}

	return matchSegments(patternSegments[1:], nameSegments[1:])
}
