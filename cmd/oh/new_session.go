package main

import (
	"crdx.org/io/cmd/oh/cycle"
	"crdx.org/io/cmd/oh/model"
)

const sourceSessionOption = "--from"

func newSessionTransition(modelGlob string, currentEffort string, choices []model.Choice) (cycle.Transition, error) {
	return selectedModelTransition(modelGlob, currentEffort, choices)
}

func forkedSessionTransition(
	modelGlob string,
	currentEffort string,
	choices []model.Choice,
	sourceSessionName string,
) (cycle.Transition, error) {
	transition, err := selectedModelTransition(modelGlob, currentEffort, choices)
	if err != nil {
		return cycle.Transition{}, err
	}

	transition.Arguments = append(transition.Arguments, sourceSessionOption, sourceSessionName)
	return transition, nil
}

func selectedModelTransition(modelGlob string, currentEffort string, choices []model.Choice) (cycle.Transition, error) {
	transition := cycle.Transition{Kind: cycle.NewSession}
	if modelGlob == "" {
		return transition, nil
	}

	selection, err := model.ResolveQuery(modelGlob, currentEffort, choices)
	if err != nil {
		return cycle.Transition{}, err
	}

	transition.Arguments = []string{"-m", selection.String()}
	return transition, nil
}
