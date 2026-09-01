package migrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
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
	4: {migrateJournal: addLastMode},
	5: {migrateLine: addEventStatus},
	6: {},
	7: {migrateJournal: promptBytesReplaceContextFiles},
	8: {migrateJournal: dropBackgroundCapability},
	9: {migrateJournal: dropUnrunModeChange},
}

func dropUnrunModeChange(lines []map[string]json.RawMessage) ([]map[string]json.RawMessage, error) {
	tail := len(lines)
	for tail > 0 && !isTurnCompletion(lines[tail-1]) {
		tail--
	}

	announcedMode := ""
	for index := tail; index < len(lines); index++ {
		event, hasEvent, err := eventOf(lines[index])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", index+1, err)
		}

		switch {
		case hasEvent && event.Kind == caps.ModeChange:
			announcedMode = event.Text
		case hasEvent && event.Kind == agent.UserMessageEvent && announces(announcedMode, event.Text):
			announcedMode = ""
		default:
			return lines, nil
		}
	}

	return lines[:tail], nil
}

func isTurnCompletion(line map[string]json.RawMessage) bool {
	return string(line["kind"]) == `"`+string(session.TurnCompletion)+`"`
}

func announces(mode string, text string) bool {
	for flag := range strings.SplitSeq(caps.AllFlags, "") {
		notice, isSaid := caps.ModeNotice(agent.Event{Name: flag, Text: mode})
		if isSaid && notice == text {
			return true
		}
	}

	return false
}

const legacyBackgroundFlag = "b"

func dropBackgroundCapability(lines []map[string]json.RawMessage) ([]map[string]json.RawMessage, error) {
	migratedLines := make([]map[string]json.RawMessage, 0, len(lines))

	for index, line := range lines {
		event, hasEvent, err := eventOf(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", index+1, err)
		}

		if hasEvent && event.Kind == caps.ModeChange {
			if event.Name == legacyBackgroundFlag {
				continue
			}

			event.Text = withoutBackgroundFlag(event.Text)

			encodedEvent, err := json.Marshal(event)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", index+1, err)
			}
			line["event"] = encodedEvent
		}

		migratedLines = append(migratedLines, line)
	}

	return migratedLines, nil
}

func withoutBackgroundFlag(flags string) string {
	return strings.ReplaceAll(flags, legacyBackgroundFlag, "")
}

func promptBytesReplaceContextFiles(lines []map[string]json.RawMessage) ([]map[string]json.RawMessage, error) {
	promptBytes, err := systemPromptBytes(lines[0])
	if err != nil {
		return nil, err
	}

	for index, line := range lines {
		err := with(line, func(event map[string]json.RawMessage) error {
			if string(event["kind"]) != `"`+string(agent.StartupEvent)+`"` {
				return nil
			}

			return restateStartupContext(event, promptBytes)
		})
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", index+1, err)
		}
	}

	return lines, nil
}

func restateStartupContext(event map[string]json.RawMessage, promptBytes int) error {
	raw, ok := event["state"]
	if !ok {
		return nil
	}

	var state map[string]json.RawMessage
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("the startup facts could not be read: %w", err)
	}

	if promptBytes == 0 {
		recordedContext, err := contextFileBytes(state)
		if err != nil {
			return err
		}
		promptBytes = recordedContext
	}

	delete(state, "context")
	if promptBytes > 0 {
		state["prompt"] = json.RawMessage(strconv.Itoa(promptBytes))
	}

	restatedState, err := json.Marshal(state)
	if err != nil {
		return err
	}
	event["state"] = restatedState

	return nil
}

func contextFileBytes(state map[string]json.RawMessage) (int, error) {
	raw, ok := state["context"]
	if !ok {
		return 0, nil
	}

	var files []struct {
		Bytes int `json:"bytes"`
	}
	if err := json.Unmarshal(raw, &files); err != nil {
		return 0, fmt.Errorf("the startup context files could not be read: %w", err)
	}

	total := 0
	for _, file := range files {
		total += file.Bytes
	}

	return total, nil
}

func systemPromptBytes(head map[string]json.RawMessage) (int, error) {
	raw, ok := head["meta"]
	if !ok {
		return 0, nil
	}

	var meta store.Meta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return 0, fmt.Errorf("the session metadata could not be read: %w", err)
	}

	return len(meta.SystemPrompt), nil
}

func addEventStatus(line map[string]json.RawMessage) error {
	return with(line, func(event map[string]json.RawMessage) error {
		var status agent.Status
		switch string(event["kind"]) {
		case `"harness_message"`:
			status = agent.WarningStatus
		case `"tool_call_result"`:
			status = agent.SuccessStatus
		default:
			return nil
		}
		if string(event["failed"]) == "true" {
			status = agent.ErrorStatus
		}

		encodedStatus, err := json.Marshal(status)
		if err != nil {
			return err
		}

		delete(event, "failed")
		event["status"] = encodedStatus
		return nil
	})
}

func addSessionMeta(directory string, name string) error {
	return store.RebuildMeta(directory, name)
}

func addLastMode(lines []map[string]json.RawMessage) ([]map[string]json.RawMessage, error) {
	currentCaps, err := initialMode(lines[0])
	if err != nil {
		return nil, err
	}

	migratedLines := make([]map[string]json.RawMessage, 0, len(lines)+1)
	lastTime := lines[0]["time"]

	for index, line := range lines {
		if at, ok := line["time"]; ok {
			lastTime = at
		}

		event, hasEvent, err := eventOf(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", index+1, err)
		}
		if hasEvent && event.Kind == caps.ModeChange {
			if event.Name == "" {
				continue
			}

			currentCaps, err = caps.Parse(withoutBackgroundFlag(event.Text))
			if err != nil {
				return nil, fmt.Errorf("line %d: the mode could not be read: %w", index+1, err)
			}
		}

		texts, err := providerUserTexts(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", index+1, err)
		}
		for _, text := range texts {
			currentCaps = modeAfterNotice(currentCaps, text)
		}

		migratedLines = append(migratedLines, line)
	}

	encodedEvent, err := json.Marshal(caps.ModeEvent(currentCaps))
	if err != nil {
		return nil, err
	}
	migratedLines = append(migratedLines, map[string]json.RawMessage{
		"kind":  json.RawMessage(`"event"`),
		"time":  lastTime,
		"event": encodedEvent,
	})

	return migratedLines, nil
}

func initialMode(head map[string]json.RawMessage) (caps.Set, error) {
	var meta store.Meta
	if raw, ok := head["meta"]; ok {
		if err := json.Unmarshal(raw, &meta); err != nil {
			return 0, fmt.Errorf("the session metadata could not be read: %w", err)
		}
	}

	currentCaps := caps.Read
	for line := range strings.SplitSeq(meta.SystemPrompt, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "- The workspace (") && strings.HasSuffix(line, " is read-write"):
			currentCaps |= caps.Write
		case strings.HasPrefix(line, "- The .git directory within it (") && strings.HasSuffix(line, " is read-write"):
			currentCaps |= caps.Git
		case line == "- The bash tool is granted":
			currentCaps |= caps.Shell
		}
	}

	return currentCaps, nil
}

func providerUserTexts(line map[string]json.RawMessage) ([]string, error) {
	if string(line["kind"]) != `"item"` {
		return nil, nil
	}

	var message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(line["payload"], &message); err != nil {
		return nil, fmt.Errorf("the provider item could not be read: %w", err)
	}
	if message.Role != "user" {
		return nil, nil
	}

	var text string
	if json.Unmarshal(message.Content, &text) == nil {
		return []string{text}, nil
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(message.Content, &blocks); err != nil {
		return nil, fmt.Errorf("the user content could not be read: %w", err)
	}

	var texts []string
	for _, block := range blocks {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}
	return texts, nil
}

func modeAfterNotice(currentCaps caps.Set, text string) caps.Set {
	if strings.Contains(text, "The workspace is now read-write.") {
		currentCaps |= caps.Write
	}
	if strings.Contains(text, "The workspace is now read-only.") {
		currentCaps &^= caps.Write
	}
	if strings.Contains(text, "The bash tool can now run shell commands.") {
		currentCaps |= caps.Shell
	}
	if strings.Contains(text, "The bash tool is now refused, and will turn away every command until it is granted again.") {
		currentCaps &^= caps.Shell
	}
	if strings.Contains(text, "The .git directory is now read-write.") {
		currentCaps |= caps.Git
	}
	if strings.Contains(text, "The .git directory is now read-only.") {
		currentCaps &^= caps.Git
	}
	return currentCaps
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

	migratedLines := make([]map[string]json.RawMessage, 0, len(lines)+len(completionAfter))
	for index, line := range lines {
		migratedLines = append(migratedLines, line)
		if completionAfter[index] {
			migratedLines = append(migratedLines, map[string]json.RawMessage{
				"kind": json.RawMessage(`"turn_completion"`),
				"time": line["time"],
			})
		}
	}
	return migratedLines, nil
}

func markMigratedTurnCompletion(lines []map[string]json.RawMessage, start int, end int, completionAfter map[int]bool) {
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
		if err == nil && hasEvent && event.Kind == agent.HarnessMessageEvent && legacyEventFailed(lines[index]) &&
			strings.Contains(event.Text, "conversation state could not be stored") {
			hasProviderStateFailure = true
		}
	}

	if lastItem >= 0 && !hasProviderStateFailure {
		completionAfter[lastItem] = true
	}
}

func legacyEventFailed(line map[string]json.RawMessage) bool {
	var event map[string]json.RawMessage
	if json.Unmarshal(line["event"], &event) != nil {
		return false
	}
	return string(event["failed"]) == "true"
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
