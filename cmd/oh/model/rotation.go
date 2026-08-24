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

func ReserveRoundRobin(path string, selections []Selection) (Selection, error) {
	if len(selections) == 0 {
		return Selection{}, errors.New("no model selected: use -m provider/model@effort or configure model.round_robin")
	}
	if len(selections) == 1 {
		return selections[0], nil
	}

	var selected Selection
	err := state.Update(path, func(state *roundRobinState) error {
		if state.Version != 0 && state.Version != roundRobinStateVersion {
			return fmt.Errorf("model round-robin state has version %d, expected %d", state.Version, roundRobinStateVersion)
		}

		selected = roundrobin.Next(selections, state.Last)
		*state = roundRobinState{Version: roundRobinStateVersion, Last: selected}

		return nil
	})

	return selected, err
}

func ParseRoundRobin(path string, writtenSelections []string) ([]Selection, error) {
	selections := make([]Selection, 0, len(writtenSelections))
	seen := make(map[string]struct{}, len(writtenSelections))

	for _, written := range writtenSelections {
		providerName, model, effort, err := ParseSelection(path, written)
		if err != nil {
			return nil, fmt.Errorf("model.round_robin: %q: %w", written, err)
		}

		selection := Selection{Provider: providerName, Model: model, Effort: effort}
		canonical := selection.String()
		if _, found := seen[canonical]; found {
			return nil, fmt.Errorf("model.round_robin selects %s more than once", canonical)
		}

		seen[canonical] = struct{}{}
		selections = append(selections, selection)
	}

	return selections, nil
}
