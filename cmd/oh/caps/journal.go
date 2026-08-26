package caps

import (
	"slices"

	"crdx.org/io/agent"
)

const ModeChange agent.Kind = "mode_change"

func ModeEvent(grantedCaps Set) agent.Event {
	return agent.Event{Kind: ModeChange, Text: grantedCaps.Flags()}
}

func ModeToggleEvent(swappedCaps Set, grantedCaps Set) agent.Event {
	return agent.Event{
		Kind: ModeChange,
		Name: swappedCaps.Flag(),
		Text: grantedCaps.Flags(),
	}
}

func ModeNotice(event agent.Event) (string, bool) {
	swappedCaps, known := Named(event.Name)
	if !known {
		return "", false
	}

	grantedCaps, err := Parse(event.Text)
	if err != nil {
		return "", false
	}

	notice := lexicalDiff(swappedCaps, grantedCaps)

	return notice, notice != ""
}

func ModeWithout(event agent.Event, swappedCaps Set) agent.Event {
	grantedCaps, err := Parse(event.Text)
	if err != nil {
		return event
	}

	event.Text = (grantedCaps ^ swappedCaps).Flags()

	return event
}

func LastRecordedMode(events []agent.Event) (Set, bool) {
	for _, event := range slices.Backward(events) {
		if event.Kind != ModeChange {
			continue
		}

		if grantedCaps, err := Parse(event.Text); err == nil {
			return grantedCaps, true
		}
	}

	return 0, false
}
