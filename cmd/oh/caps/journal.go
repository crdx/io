package caps

import (
	"encoding/json"
	"fmt"

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

const JobStop agent.Kind = "job_stop"

func JobStopEvent(jobName string, withdrawnCaps Set) agent.Event {
	return agent.Event{Kind: JobStop, Name: jobName, State: encodeFlags(withdrawnCaps)}
}

type jobStopReason struct {
	Path string `json:"path,omitempty"`
}

func JobStoppedForPathEvent(jobName string, path string) agent.Event {
	encodedReason, err := json.Marshal(jobStopReason{Path: path})
	if err != nil {
		return agent.Event{}
	}

	return agent.Event{Kind: JobStop, Name: jobName, State: encodedReason}
}

func JobStoppedByUserEvent(jobName string) agent.Event {
	return agent.Event{Kind: JobStop, Name: jobName}
}

func JobStopNotice(event agent.Event) (string, bool) {
	if event.Name == "" {
		return "", false
	}

	if len(event.State) == 0 {
		return fmt.Sprintf("The job %s was stopped from the keyboard.", event.Name), true
	}

	var stopReason jobStopReason
	if err := json.Unmarshal(event.State, &stopReason); err == nil && stopReason.Path != "" {
		return fmt.Sprintf(
			"The job %s was stopped because access to %s was revoked.", event.Name, stopReason.Path,
		), true
	}

	withdrawnCaps, err := GrantedBy(event)
	if err != nil {
		return "", false
	}

	reason := withdrawal(withdrawnCaps)
	if reason == "" {
		return "", false
	}

	return fmt.Sprintf("The job %s was stopped because %s.", event.Name, reason), true
}
