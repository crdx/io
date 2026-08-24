package migrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/session"
)

type step struct {
	migrateLine    func(line map[string]json.RawMessage) error
	migrateJournal func(lines []map[string]json.RawMessage) ([]map[string]json.RawMessage, error)
	finalise       func(directory, name string) error
}

var steps = map[int]step{
	1: {migrateLine: emphasisReplacesHighlight},
	2: {finalise: addSessionMeta},
	3: {migrateJournal: addTurnCompletions},
}

func addSessionMeta(directory, name string) error {
	return store.RebuildMeta(directory, name)
}

func emphasisReplacesHighlight(line map[string]json.RawMessage) error {
	return with(line, func(event map[string]json.RawMessage) error {
		highlight, ok := event["highlight"]
		if !ok {
			return nil
		}

		delete(event, "highlight")
		event["emphasis"] = highlight

		return nil
	})
}

func addTurnCompletions(lines []map[string]json.RawMessage) ([]map[string]json.RawMessage, error) {
	completionAfter := map[int]bool{}
	turnStart := -1

	for index, line := range lines {
		event, hasEvent, err := eventOf(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", index+1, err)
		}
		if !hasEvent || event.Kind != agent.UserMessageEvent {
			continue
		}

		markMigratedTurnCompletion(lines, turnStart, index, completionAfter)
		turnStart = index
	}
	markMigratedTurnCompletion(lines, turnStart, len(lines), completionAfter)

	migrated := make([]map[string]json.RawMessage, 0, len(lines)+len(completionAfter))
	for index, line := range lines {
		migrated = append(migrated, line)
		if completionAfter[index] {
			migrated = append(migrated, map[string]json.RawMessage{
				"kind": json.RawMessage(`"turn_completion"`),
				"time": line["time"],
			})
		}
	}
	return migrated, nil
}

func markMigratedTurnCompletion(lines []map[string]json.RawMessage, start, end int, completionAfter map[int]bool) {
	if start < 0 {
		return
	}

	lastItem := -1
	hasProviderStateFailure := false
	for index := start; index < end; index++ {
		var kind session.Kind
		if json.Unmarshal(lines[index]["kind"], &kind) != nil {
			continue
		}
		if kind == session.Item {
			lastItem = index
		}

		event, hasEvent, err := eventOf(lines[index])
		if err == nil && hasEvent && event.Kind == agent.HarnessMessageEvent && event.Failed &&
			strings.Contains(event.Text, "conversation state could not be stored") {
			hasProviderStateFailure = true
		}
	}

	if lastItem >= 0 && !hasProviderStateFailure {
		completionAfter[lastItem] = true
	}
}

func eventOf(line map[string]json.RawMessage) (agent.Event, bool, error) {
	raw, ok := line["event"]
	if !ok {
		return agent.Event{}, false, nil
	}

	var event agent.Event
	if err := json.Unmarshal(raw, &event); err != nil {
		return agent.Event{}, false, fmt.Errorf("the event could not be read: %w", err)
	}
	return event, true, nil
}

func with(line map[string]json.RawMessage, visit func(map[string]json.RawMessage) error) error {
	raw, ok := line["event"]
	if !ok {
		return nil
	}

	var event map[string]json.RawMessage
	if err := json.Unmarshal(raw, &event); err != nil {
		return fmt.Errorf("the event could not be read: %w", err)
	}

	before, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if err := visit(event); err != nil {
		return err
	}

	after, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if !bytes.Equal(before, after) {
		line["event"] = after
	}

	return nil
}
