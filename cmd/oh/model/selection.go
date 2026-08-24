package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

type Choice struct {
	Provider            string
	Model               string
	EffortLevels        []string
	ContextWindowTokens int
	MaxOutputTokens     int
}

func Chosen(path string, providerName string, model string) (Choice, error) {
	for _, choice := range Choices(path) {
		if choice.Provider == providerName && choice.Model == model {
			return choice, nil
		}
	}

	return Choice{}, fmt.Errorf(
		"nothing is known about %s/%s: run with -u to update the model list", providerName, model,
	)
}

func Choices(path string) []Choice {
	return availableModelChoices(loadModelCache(path))
}

func ParseSelection(path string, selection string) (string, string, string, error) {
	modelQuery, effortQuery, found := strings.Cut(selection, "@")
	if !found || modelQuery == "" || effortQuery == "" || strings.Contains(effortQuery, "@") {
		return "", "", "", fmt.Errorf("model must be written as provider/model@effort, got %q", selection)
	}

	choice, err := matchModel(modelQuery, Choices(path))
	if err != nil {
		return "", "", "", err
	}

	levels := EffortsMatching(effortQuery, choice.EffortLevels)
	switch len(levels) {
	case 0:
		return "", "", "", fmt.Errorf("effort %q does not match any of: %s", effortQuery, strings.Join(choice.EffortLevels, ", "))
	case 1:
		return choice.Provider, choice.Model, levels[0], nil
	default:
		return "", "", "", fmt.Errorf("effort %q is ambiguous; matches: %s", effortQuery, strings.Join(levels, ", "))
	}
}

var effortAliases = []struct {
	name  string // what a caller may type
	level string // the level it means
}{
	{name: "off", level: "none"},
}

func EffortsMatching(query string, efforts []string) []string {
	names := slices.Clone(efforts)

	for _, alias := range effortAliases {
		if slices.Contains(efforts, alias.level) {
			names = append(names, alias.name)
		}
	}

	var levels []string

	for _, name := range matchPrefixes(query, names) {
		if level := ResolveEffort(name); !slices.Contains(levels, level) {
			levels = append(levels, level)
		}
	}

	return levels
}

func ResolveEffort(name string) string {
	for _, alias := range effortAliases {
		if alias.name == strings.ToLower(name) {
			return alias.level
		}
	}

	return name
}

var matchTiers = []func(candidate string, query string) bool{
	func(candidate string, query string) bool { return candidate == query },
	strings.HasPrefix,
	strings.Contains,
	holdsInOrder,
}

func matchModel(query string, choices []Choice) (Choice, error) {
	if len(choices) == 0 {
		return Choice{}, errors.New("no models are known: run with -u to fetch the model list")
	}

	matches := RankedChoices(query, choices)

	switch len(matches) {
	case 0:
		return Choice{}, fmt.Errorf("model %q does not match any known model", query)
	case 1:
		return matches[0], nil
	default:
		names := make([]string, len(matches))
		for i, choice := range matches {
			names[i] = choice.Provider + "/" + choice.Model
		}
		return Choice{}, fmt.Errorf("model %q is ambiguous; matches: %s", query, strings.Join(names, ", "))
	}
}

func RankedChoices(query string, choices []Choice) []Choice {
	for _, matching := range matchTiers {
		if matches := matchingModels(query, choices, matching); len(matches) > 0 {
			return matches
		}
	}

	return nil
}

func matchingModels(
	query string,
	choices []Choice,
	matching func(candidate string, query string) bool,
) []Choice {
	query = strings.ToLower(query)

	var matches []Choice

	for _, choice := range choices {
		model := strings.ToLower(choice.Model)
		qualified := strings.ToLower(choice.Provider + "/" + choice.Model)

		if matching(model, query) || matching(qualified, query) {
			matches = append(matches, choice)
		}
	}

	return matches
}

func matchPrefixes(query string, candidates []string) []string {
	query = strings.ToLower(query)
	var matches []string
	for _, candidate := range candidates {
		if strings.HasPrefix(strings.ToLower(candidate), query) {
			matches = append(matches, candidate)
		}
	}
	return matches
}

func holdsInOrder(candidate string, query string) bool {
	wanted := []rune(query)

	var matched int

	for _, letter := range candidate {
		if matched < len(wanted) && letter == wanted[matched] {
			matched++
		}
	}

	return matched == len(wanted)
}
