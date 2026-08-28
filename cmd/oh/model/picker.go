package model

import (
	"errors"
	"io"
	"os"
	"slices"
	"strings"

	"crdx.org/io/cmd/oh/modelPicker"
	"crdx.org/io/cmd/oh/tty"
)

var ErrNotLoggedIn = errors.New("not logged in to any provider: run oh -L to sign in")

func Choose(
	path string,
	isLoggedIn func(providerName string) bool,
	terminal *os.File,
	screen io.Writer,
) (Selection, error) {
	choices := Choices(path)
	if len(choices) == 0 {
		return Selection{}, errors.New("no models are known: run with -u to fetch the model list")
	}

	if choices = signedInto(choices, isLoggedIn); len(choices) == 0 {
		return Selection{}, ErrNotLoggedIn
	}

	chosen, err := modelPicker.Choose(offered(choices, defaultEffort), terminal, screen)
	if err != nil {
		return Selection{}, err
	}

	return Selection{Provider: chosen.ProviderID, Model: chosen.ID, Effort: chosen.Effort}, nil
}

func ChooseWhenNoneSelected(
	reason error,
	path string,
	isLoggedIn func(providerName string) bool,
	terminal *os.File,
	screen io.Writer,
) (Selection, error) {
	if !errors.Is(reason, ErrNoSelection) || !tty.Is(terminal) || !tty.Is(screen) {
		return Selection{}, reason
	}

	return Choose(path, isLoggedIn, terminal, screen)
}

func signedInto(choices []Choice, isLoggedIn func(providerName string) bool) []Choice {
	available := make([]Choice, 0, len(choices))

	for _, choice := range choices {
		if isLoggedIn(choice.Provider) {
			available = append(available, choice)
		}
	}

	return available
}

func offered(choices []Choice, currentEffort string) []*modelPicker.Model {
	models := make([]*modelPicker.Model, 0, len(choices))

	for _, choice := range choices {
		efforts := orderedEfforts(choice.EffortLevels)
		effort := NearestEffort(currentEffort, efforts)
		if effort == "" {
			effort = efforts[0]
		}

		models = append(models, &modelPicker.Model{
			Provider:            ProviderName(choice.Provider),
			ProviderID:          choice.Provider,
			Name:                strings.Join(DisplayName(choice.Model), " "),
			ID:                  choice.Model,
			EffortLevels:        efforts,
			Effort:              effort,
			ContextWindowTokens: choice.ContextWindowTokens,
		})
	}

	return models
}

func orderedEfforts(efforts []string) []string {
	ordered := slices.Clone(efforts)

	slices.SortStableFunc(ordered, func(first string, second string) int {
		return slices.Index(EffortOrder, first) - slices.Index(EffortOrder, second)
	})

	return ordered
}
