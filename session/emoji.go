package session

import "strings"

// Emoji returns the emoji standing for the animal a session is named after, or the empty string if
// the name carries no animal that has one.
func Emoji(name string) string {
	_, animal, found := strings.Cut(name, "-")
	if !found {
		return ""
	}

	return animalEmojis[animal]
}
