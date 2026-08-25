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

	if !isDrivable(providerName, model) {
		return Choice{}, fmt.Errorf(
			"%s/%s is not supported: it takes a request shape this build does not speak",
			providerName, model,
		)
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

	effort, err := matchEffort(effortQuery, choice)
	if err != nil {
		return "", "", "", err
	}

	return choice.Provider, choice.Model, effort, nil
}

func ResolveQuery(query string, currentEffort string, choices []Choice) (Selection, error) {
	modelQuery, effortQuery, hasEffort := strings.Cut(query, "@")
	if modelQuery == "" || (hasEffort && effortQuery == "") {
		return Selection{}, fmt.Errorf("model must be written as model or model@effort, got %q", query)
	}

	choice, err := onlyMatch(modelQuery, RankedChoicesWithoutGuessing(modelQuery, choices))
	if err != nil {
		return Selection{}, err
	}

	effort := NearestEffort(currentEffort, choice.EffortLevels)

	if hasEffort {
		if effort, err = matchEffort(effortQuery, choice); err != nil {
			return Selection{}, err
		}
	} else if effort == "" {
		return Selection{}, fmt.Errorf("model %s has no recognised effort levels", choice.Model)
	}

	return Selection{Provider: choice.Provider, Model: choice.Model, Effort: effort}, nil
}

var effortOrder = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

func NearestEffort(current string, available []string) string {
	currentIndex := slices.Index(effortOrder, current)

	for distance := range effortOrder {
		higherIndex := currentIndex + distance
		if higherIndex >= 0 && higherIndex < len(effortOrder) && slices.Contains(available, effortOrder[higherIndex]) {
			return effortOrder[higherIndex]
		}

		lowerIndex := currentIndex - distance
		if distance > 0 && lowerIndex >= 0 && slices.Contains(available, effortOrder[lowerIndex]) {
			return effortOrder[lowerIndex]
		}
	}

	return ""
}

func matchEffort(query string, choice Choice) (string, error) {
	levels := EffortsMatching(query, choice.EffortLevels)

	switch len(levels) {
	case 0:
		return "", fmt.Errorf("effort %q does not match any of: %s", query, strings.Join(choice.EffortLevels, ", "))
	case 1:
		return levels[0], nil
	default:
		return "", fmt.Errorf("effort %q is ambiguous; matches: %s", query, strings.Join(levels, ", "))
	}
}

var effortAliases = []struct {
	name  string
	level string
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

type matcher func(candidate string, query string) bool

var certainMatchTiers = []matcher{
	func(candidate string, query string) bool { return candidate == query },
	strings.HasPrefix,
	strings.Contains,
}

var matchTiers = slices.Concat(certainMatchTiers, []matcher{holdsInOrder})

func matchModel(query string, choices []Choice) (Choice, error) {
	if len(choices) == 0 {
		return Choice{}, errors.New("no models are known: run with -u to fetch the model list")
	}

	return onlyMatch(query, RankedChoices(query, choices))
}

func onlyMatch(query string, matches []Choice) (Choice, error) {
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
	return rankedChoices(query, choices, matchTiers)
}

func RankedChoicesWithoutGuessing(query string, choices []Choice) []Choice {
	return rankedChoices(query, choices, certainMatchTiers)
}

func rankedChoices(query string, choices []Choice, tiers []matcher) []Choice {
	for _, matching := range tiers {
		if matches := matchingModels(query, choices, matching); len(matches) > 0 {
			return matches
		}
	}

	return nil
}

func matchingModels(query string, choices []Choice, matching matcher) []Choice {
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
