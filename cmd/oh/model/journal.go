package model

import (
	"encoding/json"
	"slices"

	"crdx.org/io/agent"
)

const FastModeStateKey = "fast-mode"

type fastModeState struct {
	IsFast bool `json:"fast"`
}

func FastModeEvent(isFast bool) agent.Event {
	state := json.RawMessage(`{"fast":false}`)
	if isFast {
		state = json.RawMessage(`{"fast":true}`)
	}

	return agent.Event{Kind: agent.StateChangeEvent, Name: FastModeStateKey, State: state}
}

func LastRecordedFastMode(events []agent.Event) (bool, bool) {
	for _, event := range slices.Backward(events) {
		if event.Kind != agent.StateChangeEvent || event.Name != FastModeStateKey {
			continue
		}

		var state fastModeState
		if err := json.Unmarshal(event.State, &state); err == nil {
			return state.IsFast, true
		}
	}

	return false, false
}
