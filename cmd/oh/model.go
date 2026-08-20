package main

import (
	"fmt"
	"strings"

	"crdx.org/io/provider/codex"
)

type modelChoice struct {
	provider string
	model    string
	efforts  []string
}

var modelChoices = []modelChoice{
	{provider: codexProvider, model: codex.Model, efforts: []string{"minimal", "low", "medium", "high"}},
	{provider: opencodeGoProvider, model: "deepseek-v4-pro", efforts: []string{"minimal", "low", "medium", "high"}},
}

func parseModelSelection(selection string) (string, string, string, error) {
	modelQuery, effortQuery, found := strings.Cut(selection, "@")
	if !found || modelQuery == "" || effortQuery == "" || strings.Contains(effortQuery, "@") {
		return "", "", "", fmt.Errorf("model must be written as provider/model@effort, got %q", selection)
	}

	choice, err := matchModel(modelQuery, modelChoices)
	if err != nil {
		return "", "", "", err
	}

	effortMatches := matchPrefixes(effortQuery, choice.efforts)
	switch len(effortMatches) {
	case 0:
		return "", "", "", fmt.Errorf("effort %q does not match any of: %s", effortQuery, strings.Join(choice.efforts, ", "))
	case 1:
		return choice.provider, choice.model, effortMatches[0], nil
	default:
		return "", "", "", fmt.Errorf("effort %q is ambiguous; matches: %s", effortQuery, strings.Join(effortMatches, ", "))
	}
}

func matchModel(query string, choices []modelChoice) (modelChoice, error) {
	matches := matchingModels(query, choices, true)
	if len(matches) == 0 {
		matches = matchingModels(query, choices, false)
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

func matchingModels(query string, choices []modelChoice, exact bool) []modelChoice {
	query = strings.ToLower(query)
	var matches []modelChoice
	for _, choice := range choices {
		model := strings.ToLower(choice.model)
		qualified := strings.ToLower(choice.provider + "/" + choice.model)
		if exact && (query == model || query == qualified) {
			matches = append(matches, choice)
		}
		if !exact && (fuzzyMatch(query, model) || fuzzyMatch(query, qualified)) {
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

func fuzzyMatch(query string, candidate string) bool {
	if strings.Contains(candidate, query) {
		return true
	}

	queryRunes := []rune(query)
	matched := 0
	for _, candidateRune := range candidate {
		if matched < len(queryRunes) && candidateRune == queryRunes[matched] {
			matched++
		}
	}
	return matched == len(queryRunes)
}
