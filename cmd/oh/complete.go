package main

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/models"
	"crdx.org/io/session"
)

const completeFlag = "--complete"

const (
	completeOption  = "option"
	completeModel   = "model"
	completeEffort  = "effort"
	completeSession = "session"
	completeCaps    = "caps"
)

func completionRequest(args []string) (string, string, bool) {
	if len(args) == 0 || args[0] != completeFlag {
		return "", "", false
	}

	rest := args[1:]
	if len(rest) == 0 {
		return "", "", false
	}

	var word string
	if len(rest) > 1 {
		word = rest[1]
	}

	return rest[0], word, true
}

func writeCompletions(out io.Writer, kind string, word string) {
	for _, completion := range completions(kind, word) {
		_, _ = fmt.Fprintln(out, completion)
	}
}

func completions(kind string, word string) []string {
	switch kind {
	case completeOption:
		return optionCompletions(word, usageOptions(usage))
	case completeModel:
		return modelCompletions(word, models.Choices(modelCachePath()))
	case completeEffort:
		return effortCompletions(word, models.Choices(modelCachePath()))
	case completeSession:
		return withPrefix(word, sessionNames(sessionsDir()))
	case completeCaps:
		return withPrefix(word, capsCompletions())
	default:
		return nil
	}
}

func withPrefix(word string, candidates []string) []string {
	var matches []string

	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, word) {
			matches = append(matches, candidate)
		}
	}

	return matches
}

func optionCompletions(word string, options []string) []string {
	if word == "" {
		return withPrefix("--", options)
	}

	return withPrefix(word, options)
}

func usageOptions(text string) []string {
	var options []string

	for line := range strings.SplitSeq(text, "\n") {
		for field := range strings.FieldsSeq(line) {
			name := strings.TrimSuffix(field, ",")
			if !strings.HasPrefix(name, "-") || name == "-" {
				break
			}
			options = append(options, name)
		}
	}

	slices.Sort(options)

	return options
}

func modelCompletions(word string, choices []models.Choice) []string {
	modelQuery, effortQuery, qualified := strings.Cut(word, "@")

	var selections []string

	for _, choice := range models.RankedChoices(modelQuery, choices) {
		efforts := choice.EffortLevels
		if qualified {
			efforts = models.EffortsMatching(effortQuery, choice.EffortLevels)
		}

		for _, effort := range efforts {
			selections = append(selections, choice.Provider+"/"+choice.Model+"@"+effort)
		}
	}

	return selections
}

func effortCompletions(word string, choices []models.Choice) []string {
	modelQuery, effortQuery, _ := strings.Cut(word, "@")

	var efforts []string

	for _, choice := range models.RankedChoices(modelQuery, choices) {
		for _, effort := range models.EffortsMatching(effortQuery, choice.EffortLevels) {
			if !slices.Contains(efforts, effort) {
				efforts = append(efforts, effort)
			}
		}
	}

	return efforts
}

func sessionNames(directory string) []string {
	entries, err := session.Entries(directory)
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}

	slices.Reverse(names)

	return names
}

func capsCompletions() []string {
	sets := make([]string, 0, len(caps.AllFlags))

	for i := range caps.AllFlags {
		sets = append(sets, caps.AllFlags[:i+1])
	}

	return sets
}
