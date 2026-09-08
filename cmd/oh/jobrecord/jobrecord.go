package jobrecord

import (
	"encoding/json"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/access"
	"crdx.org/io/internal/jobs"
)

const Listing agent.Kind = "job_listing"

func ListingEvent(listing []jobs.Snapshot) agent.Event {
	encodedListing, err := json.Marshal(listing)
	if err != nil {
		return agent.Event{}
	}

	return agent.Event{Kind: Listing, State: encodedListing}
}

func decodeListing(event agent.Event) ([]jobs.Snapshot, error) {
	var listing []jobs.Snapshot
	if err := json.Unmarshal(event.State, &listing); err != nil {
		return nil, err
	}

	return listing, nil
}

func LastRecorded(events []agent.Event) ([]jobs.Snapshot, bool) {
	return access.LastRecorded(events, Listing, decodeListing)
}

const Ended agent.Kind = "job_ended"

func EndedEvent(snapshot jobs.Snapshot) agent.Event {
	encodedSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		return agent.Event{}
	}

	return agent.Event{Kind: Ended, Name: snapshot.Name, State: encodedSnapshot}
}

func EndedNotice(event agent.Event) (string, bool) {
	var snapshot jobs.Snapshot
	if err := json.Unmarshal(event.State, &snapshot); err != nil || snapshot.Name == "" {
		return "", false
	}

	return "The job " + snapshot.Name + " exited: " + snapshot.Outcome() + ". Read its output with the job tool.", true
}

const EndedWithSession agent.Kind = "jobs_ended_with_session"

func EndedWithSessionEvent(names []string) agent.Event {
	encodedNames, err := json.Marshal(names)
	if err != nil {
		return agent.Event{}
	}

	return agent.Event{Kind: EndedWithSession, State: encodedNames}
}

func EndedWithSessionNotice(event agent.Event) (string, bool) {
	var names []string
	if err := json.Unmarshal(event.State, &names); err != nil || len(names) == 0 {
		return "", false
	}

	subject := "The job " + names[0] + " is"
	if len(names) > 1 {
		subject = "The jobs " + strings.Join(names[:len(names)-1], ", ") +
			" and " + names[len(names)-1] + " are"
	}

	return subject + " no longer running, having been stopped when the session last closed. " +
		"Start one again by name to run its command afresh.", true
}
