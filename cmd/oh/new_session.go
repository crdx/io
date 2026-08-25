package main

import (
	"crdx.org/io/cmd/oh/cycle"
	"crdx.org/io/cmd/oh/model"
)

const sourceSessionOption = "--from"

func newSessionTransition(workspaceDir string, modelGlob string, currentEffort string, choices []model.Choice) (cycle.Transition, error) {
	arguments := []string{"-d", workspaceDir}
	if modelGlob == "" {
		return cycle.Transition{Kind: cycle.NewSession, Arguments: arguments}, nil
	}

	selection, err := model.ResolveQuery(modelGlob, currentEffort, choices)
	if err != nil {
		return cycle.Transition{}, err
	}

	arguments = append(arguments, "-m", selection.String())

	return cycle.Transition{Kind: cycle.NewSession, Arguments: arguments}, nil
}

func forkedSessionTransition(
	workspaceDir string,
	modelGlob string,
	currentEffort string,
	choices []model.Choice,
	sourceSessionName string,
) (cycle.Transition, error) {
	transition, err := newSessionTransition(workspaceDir, modelGlob, currentEffort, choices)
	if err != nil {
		return cycle.Transition{}, err
	}
	transition.Arguments = append(
		transition.Arguments,
		sourceSessionOption,
		sourceSessionName,
	)
	return transition, nil
}
