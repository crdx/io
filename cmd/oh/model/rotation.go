package model

import (
	"errors"
	"fmt"

	"crdx.org/io/internal/roundrobin"
	"crdx.org/io/internal/state"
)

type Selection struct {
	Provider string
	Model    string
	Effort   string
}

func (self Selection) String() string {
	return self.Provider + "/" + self.Model + "@" + self.Effort
}

const roundRobinStateVersion = 1

type roundRobinState struct {
	Version int       `json:"version"`
	Last    Selection `json:"last"`
}

var ErrNoSelection = errors.New(
	"no model selected: use -m provider/model@effort or configure model.round_robin",
)

func ReserveRoundRobin(path string, selections []Selection) (Selection, error) {
	if len(selections) == 0 {
		return Selection{}, ErrNoSelection
	}
	if len(selections) == 1 {
		return selections[0], nil
	}

	var selectedModel Selection
	err := state.Update(path, roundRobinStateVersion, func(storedState *roundRobinState) error {
		if storedState.Version != 0 && storedState.Version != roundRobinStateVersion {
			return fmt.Errorf("model round-robin state has version %d, expected %d", storedState.Version, roundRobinStateVersion)
		}

		selectedModel = roundrobin.Next(selections, storedState.Last)
		*storedState = roundRobinState{Version: roundRobinStateVersion, Last: selectedModel}

		return nil
	})

	return selectedModel, err
}

func ParseRoundRobin(path string, writtenSelections []string) ([]Selection, error) {
	selections := make([]Selection, 0, len(writtenSelections))
	seen := make(map[string]struct{}, len(writtenSelections))

	for _, writtenSelection := range writtenSelections {
		providerName, model, effort, err := ParseSelection(path, writtenSelection)
		if err != nil {
			return nil, fmt.Errorf("model.round_robin: %q: %w", writtenSelection, err)
		}

		selection := Selection{Provider: providerName, Model: model, Effort: effort}
		canonical := selection.String()
		if _, isFound := seen[canonical]; isFound {
			return nil, fmt.Errorf("model.round_robin selects %s more than once", canonical)
		}

		seen[canonical] = struct{}{}
		selections = append(selections, selection)
	}

	return selections, nil
}
