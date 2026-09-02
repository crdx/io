package caps

import (
	"encoding/json"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/access"
)

const ModeChange agent.Kind = "mode_change"

func ModeEvent(grantedCaps Set) agent.Event {
	return agent.Event{Kind: ModeChange, State: encodeFlags(grantedCaps)}
}

func ModeToggleEvent(swappedCaps Set, grantedCaps Set) agent.Event {
	return agent.Event{
		Kind:  ModeChange,
		Name:  swappedCaps.Flag(),
		State: encodeFlags(grantedCaps),
	}
}

func GrantedBy(event agent.Event) (Set, error) {
	var flags string
	if err := json.Unmarshal(event.State, &flags); err != nil {
		return 0, err
	}

	return Parse(flags)
}

func Notice(swappedCaps Set, grantedCaps Set) (string, bool) {
	notice := lexicalDiff(swappedCaps, grantedCaps)

	return notice, notice != ""
}

func ModeNotice(event agent.Event) (string, bool) {
	swappedCaps, isKnown := Named(event.Name)
	if !isKnown {
		return "", false
	}

	grantedCaps, err := GrantedBy(event)
	if err != nil {
		return "", false
	}

	return Notice(swappedCaps, grantedCaps)
}

func ModeWithout(event agent.Event, swappedCaps Set) agent.Event {
	grantedCaps, err := GrantedBy(event)
	if err != nil {
		return event
	}

	event.State = encodeFlags(grantedCaps ^ swappedCaps)

	return event
}

func LastRecordedMode(events []agent.Event) (Set, bool) {
	return access.LastRecorded(events, ModeChange, GrantedBy)
}

func encodeFlags(grantedCaps Set) json.RawMessage {
	encodedFlags, err := json.Marshal(grantedCaps.Flags())
	if err != nil {
		return nil
	}

	return encodedFlags
}
