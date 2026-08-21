package session

import (
	_ "embed"
	"strings"
)

//go:embed adjectives.txt
var adjectiveList string

//go:embed animals.txt
var animalList string

var (
	adjectives = strings.Fields(adjectiveList)
	animals    = strings.Fields(animalList)
)
