package main

import (
	"fmt"
	"slices"

	"crdx.org/io/cmd/oh/cycle"
	"crdx.org/io/cmd/oh/model"
)

func newSessionTransition(workspaceDir string, modelGlob string, currentEffort string, choices []model.Choice) (cycle.Transition, error) {
	arguments := []string{"-d", workspaceDir}
	if modelGlob == "" {
		return cycle.Transition{Kind: cycle.NewSession, Arguments: arguments}, nil
	}

	matches := model.RankedChoicesWithoutGuessing(modelGlob, choices)
	if len(matches) == 0 {
		return cycle.Transition{}, fmt.Errorf("model %q does not match any known model", modelGlob)
	}

	choice := matches[0]
	effort := nearestEffort(currentEffort, choice.EffortLevels)
	if effort == "" {
		return cycle.Transition{}, fmt.Errorf("model %s has no recognised effort levels", choice.Model)
	}
	selection := model.Selection{Provider: choice.Provider, Model: choice.Model, Effort: effort}
	arguments = append(arguments, "-m", selection.String())
	return cycle.Transition{Kind: cycle.NewSession, Arguments: arguments}, nil
}

var effortOrder = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

func nearestEffort(current string, available []string) string {
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
