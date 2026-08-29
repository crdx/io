package cycle

import "crdx.org/io/cmd/oh/model"

const sourceSessionOption = "--from"

// NewSessionTransition resolves a model query into a new-session transition.
func NewSessionTransition(modelGlob string, currentEffort string, choices []model.Choice) (Transition, error) {
	return selectedModelTransition(modelGlob, currentEffort, choices)
}

// ForkedSessionTransition resolves a model query into a transition forked from a stored session.
func ForkedSessionTransition(
	modelGlob string,
	currentEffort string,
	choices []model.Choice,
	sourceSessionName string,
) (Transition, error) {
	transition, err := selectedModelTransition(modelGlob, currentEffort, choices)
	if err != nil {
		return Transition{}, err
	}

	transition.Arguments = append(transition.Arguments, sourceSessionOption, sourceSessionName)
	return transition, nil
}

func selectedModelTransition(modelGlob string, currentEffort string, choices []model.Choice) (Transition, error) {
	transition := Transition{Kind: NewSession}
	if modelGlob == "" {
		return transition, nil
	}

	selection, err := model.ResolveQuery(modelGlob, currentEffort, choices)
	if err != nil {
		return Transition{}, err
	}

	transition.Arguments = []string{"-m", selection.String()}
	return transition, nil
}
