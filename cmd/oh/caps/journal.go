package caps

import (
	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/access"
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
	return access.LastRecorded(events, ModeChange, func(event agent.Event) (Set, error) {
		return Parse(event.Text)
	})
}
