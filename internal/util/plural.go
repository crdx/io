package util

import (
	"strconv"
)

func Plural(count int, noun string) string {
	return strconv.Itoa(count) + " " + PluralNoun(count, noun)
}

func PluralNoun(count int, noun string) string {
	if count == 1 {
		return noun
	}

	return noun + "s"
}
