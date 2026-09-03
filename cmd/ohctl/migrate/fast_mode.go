package migrate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/session"
)

func backfillFastMode(options Options, name string) (bool, error) {
	if options.DryRun {
		return backfillSessionFastMode(options, name)
	}

	wasBackfilled := false
	err := session.Unarchived(options.Directory, name, func() error {
		var err error
		wasBackfilled, err = backfillSessionFastMode(options, name)
		return err
	})

	return wasBackfilled, err
}

func backfillSessionFastMode(options Options, name string) (bool, error) {
	if !options.DryRun {
		heldLock, err := session.AcquireLock(options.Directory, name)
		if err != nil {
			return false, err
		}
		defer func() { _ = heldLock.Release() }()
	}

	journalPath := filepath.Join(options.Directory, name, "session.jsonl")
	lines, storedFormat, err := readJournal(options.Directory, name)
	if err != nil {
		return false, err
	}
	if storedFormat != session.JournalFormat {
		return false, nil
	}

	var meta store.Meta
	if encodedMeta, isFound := lines[0]["meta"]; isFound {
		if err := json.Unmarshal(encodedMeta, &meta); err != nil {
			return false, fmt.Errorf("the session metadata could not be read: %w", err)
		}
	}
	if meta.Provider != model.CodexProvider {
		return false, nil
	}

	for index, line := range lines {
		event, hasEvent, err := eventOf(line)
		if err != nil {
			return false, fmt.Errorf("line %d: %w", index+1, err)
		}
		if hasEvent && event.Kind == agent.StateChangeEvent && event.Name == model.FastModeStateKey {
			return false, nil
		}
	}

	if options.DryRun {
		return true, nil
	}

	encodedEvent, err := json.Marshal(model.FastModeEvent(true))
	if err != nil {
		return false, err
	}
	eventLine := map[string]json.RawMessage{
		"kind":  json.RawMessage(`"event"`),
		"event": encodedEvent,
	}
	insertAt := 1
	for index, line := range lines {
		event, hasEvent, err := eventOf(line)
		if err != nil {
			return false, fmt.Errorf("line %d: %w", index+1, err)
		}
		if hasEvent && event.Kind == caps.ModeChange {
			insertAt = index + 1
			if timestamp, isFound := line["time"]; isFound {
				eventLine["time"] = timestamp
			}
			break
		}
	}
	lines = slices.Insert(lines, insertAt, eventLine)

	copyPath := filepath.Join(options.BackupDir, name)
	if err := keepCopy(filepath.Join(options.Directory, name), copyPath); err != nil {
		return false, err
	}
	if err := writeJournal(journalPath, lines); err != nil {
		return false, err
	}
	if err := store.Rebuild(options.Directory, name); err != nil {
		return false, fmt.Errorf("the journal was migrated but its transcript was not: %w", err)
	}

	return true, nil
}

func fastModeNames(directory string, requestedSessionNames []string) ([]string, error) {
	if len(requestedSessionNames) > 0 {
		return slices.Clone(requestedSessionNames), nil
	}

	entries, err := session.Entries(directory)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Name
	}
	return names, nil
}
