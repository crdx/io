package conditions

import (
	"encoding/json"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/access"
	"crdx.org/io/internal/sandbox"
)

type Conditions struct {
	UnixSockets bool `json:"unix_sockets"`
	IPv6        bool `json:"ipv6"`
	Interactive bool `json:"interactive"`
}

func Probe(isYolo bool, isInteractive bool) Conditions {
	return Conditions{
		UnixSockets: isYolo || sandbox.AreUnixSocketsReachable(),
		IPv6:        sandbox.IsIPv6Reachable(),
		Interactive: isInteractive,
	}
}

type State struct {
	state *access.State[Conditions]
}

func NewRestored(current Conditions, knownConditions Conditions) *State {
	return &State{state: access.NewRestored(current, knownConditions, definition())}
}

type Restoration struct {
	State     *State
	Change    agent.Event
	IsChanged bool
}

func Restore(
	createdConditions *Conditions,
	events []agent.Event,
	current Conditions,
) (Restoration, error) {
	knownConditions := current
	if createdConditions != nil {
		knownConditions = *createdConditions
	}
	if recordedConditions, found := LastRecorded(events); found {
		knownConditions = recordedConditions
	}

	change, err := ChangeEvent(knownConditions, current)
	if err != nil {
		return Restoration{}, err
	}

	_, isChanged := Notice(change)

	return Restoration{
		State:     NewRestored(current, knownConditions),
		Change:    change,
		IsChanged: isChanged,
	}, nil
}

func (self *State) Peek() string {
	return self.state.Peek()
}

func (self *State) Inject() string {
	return self.state.Inject()
}

func definition() access.Definition[Conditions] {
	return access.Definition[Conditions]{
		Clone:    func(current Conditions) Conditions { return current },
		Describe: describeChanges,
	}
}

func describeChanges(knownConditions Conditions, current Conditions) string {
	var clauses []string

	if knownConditions.UnixSockets != current.UnixSockets {
		if current.UnixSockets {
			clauses = append(clauses,
				"A Unix socket now works beneath /tmp, and is refused beneath the workspace.")
		} else {
			clauses = append(clauses,
				"A Unix socket no longer works at any path, so no service can listen on a Unix socket.")
		}
	}

	if knownConditions.IPv6 != current.IPv6 {
		if current.IPv6 {
			clauses = append(clauses, "This machine has IPv6 again, so ::1 reaches the sandbox loopback.")
		} else {
			clauses = append(clauses, "This machine has lost IPv6, so only 127.0.0.1 works.")
		}
	}

	if knownConditions.Interactive != current.Interactive {
		if current.Interactive {
			clauses = append(clauses, "The user has returned, so you can ask them a question.")
		} else {
			clauses = append(clauses, "The user has gone, so you cannot ask them anything.")
		}
	}

	return strings.Join(clauses, " ")
}

const Change agent.Kind = "conditions_change"

type eventState struct {
	KnownConditions Conditions `json:"known"`
	Current         Conditions `json:"current"`
}

func ChangeEvent(knownConditions Conditions, current Conditions) (agent.Event, error) {
	state, err := json.Marshal(eventState{KnownConditions: knownConditions, Current: current})
	if err != nil {
		return agent.Event{}, err
	}

	return agent.Event{Kind: Change, State: state}, nil
}

func decodeEvent(event agent.Event) (Conditions, error) {
	var state eventState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return Conditions{}, err
	}

	return state.Current, nil
}

func LastRecorded(events []agent.Event) (Conditions, bool) {
	return access.LastRecorded(events, Change, decodeEvent)
}

func Summary(event agent.Event) (string, bool) {
	if event.Kind != Change {
		return "", false
	}

	var state eventState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return "", false
	}

	var facilities []string
	if state.KnownConditions.UnixSockets != state.Current.UnixSockets {
		facilities = append(facilities, "unix sockets")
	}
	if state.KnownConditions.IPv6 != state.Current.IPv6 {
		facilities = append(facilities, "ipv6")
	}
	if state.KnownConditions.Interactive != state.Current.Interactive {
		facilities = append(facilities, "asking the user")
	}

	if len(facilities) == 0 {
		return "", false
	}

	return strings.Join(facilities, ", "), true
}

func Notice(event agent.Event) (string, bool) {
	if event.Kind != Change {
		return "", false
	}

	var state eventState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return "", false
	}

	notice := describeChanges(state.KnownConditions, state.Current)

	return notice, notice != ""
}
