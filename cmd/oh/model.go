package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

type modelChoice struct {
	provider        string
	model           string
	effortLevels    []string
	maxOutputTokens int
}

func chosenModel(providerName string, model string) (modelChoice, error) {
	for _, choice := range modelChoices() {
		if choice.provider == providerName && choice.model == model {
			return choice, nil
		}
	}

	return modelChoice{}, fmt.Errorf(
		"nothing is known about %s/%s: run with -u to update the model list", providerName, model,
	)
}

func modelChoices() []modelChoice {
	return availableModelChoices(loadModelCache(modelCachePath()))
}

func parseModelSelection(selection string) (string, string, string, error) {
	modelQuery, effortQuery, found := strings.Cut(selection, "@")
	if !found || modelQuery == "" || effortQuery == "" || strings.Contains(effortQuery, "@") {
		return "", "", "", fmt.Errorf("model must be written as provider/model@effort, got %q", selection)
	}

	choice, err := matchModel(modelQuery, modelChoices())
	if err != nil {
		return "", "", "", err
	}

	levels := effortsMatching(effortQuery, choice.effortLevels)
	switch len(levels) {
	case 0:
		return "", "", "", fmt.Errorf("effort %q does not match any of: %s", effortQuery, strings.Join(choice.effortLevels, ", "))
	case 1:
		return choice.provider, choice.model, levels[0], nil
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

func effortsMatching(query string, efforts []string) []string {
	names := slices.Clone(efforts)

	for _, alias := range effortAliases {
		if slices.Contains(efforts, alias.level) {
			names = append(names, alias.name)
		}
	}

	var levels []string

	for _, name := range matchPrefixes(query, names) {
		if level := resolveEffort(name); !slices.Contains(levels, level) {
			levels = append(levels, level)
		}
	}

	return levels
}

func resolveEffort(name string) string {
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

func matchModel(query string, choices []modelChoice) (modelChoice, error) {
	if len(choices) == 0 {
		return modelChoice{}, errors.New("no models are known: run with -u to fetch the model list")
	}

	var matches []modelChoice

	for _, matching := range matchTiers {
		if matches = matchingModels(query, choices, matching); len(matches) > 0 {
			break
		}
	}

	switch len(matches) {
	case 0:
		return modelChoice{}, fmt.Errorf("model %q does not match any known model", query)
	case 1:
		return matches[0], nil
	default:
		names := make([]string, len(matches))
		for index, choice := range matches {
			names[index] = choice.provider + "/" + choice.model
		}
		return modelChoice{}, fmt.Errorf("model %q is ambiguous; matches: %s", query, strings.Join(names, ", "))
	}
}

func matchingModels(
	query string,
	choices []modelChoice,
	matching func(candidate string, query string) bool,
) []modelChoice {
	query = strings.ToLower(query)

	var matches []modelChoice

	for _, choice := range choices {
		model := strings.ToLower(choice.model)
		qualified := strings.ToLower(choice.provider + "/" + choice.model)

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
