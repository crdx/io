package migrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/interrupt"
	"crdx.org/io/cmd/oh/pathgrant"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/turn"
	"crdx.org/io/session"
)

type step struct {
	migrateLine    func(line map[string]json.RawMessage) error
	migrateJournal func(lines []map[string]json.RawMessage) ([]map[string]json.RawMessage, error)
	finalise       func(directory, name string) error
}

var steps = map[int]step{
	1:  {migrateLine: emphasisReplacesHighlight},
	2:  {finalise: addSessionMeta},
	3:  {migrateJournal: addTurnCompletions},
	4:  {migrateJournal: addLastMode},
	5:  {migrateLine: addEventStatus},
	6:  {},
	7:  {migrateJournal: promptBytesReplaceContextFiles},
	8:  {migrateJournal: dropBackgroundCapability},
	9:  {migrateJournal: dropUnrunModeChange},
	10: {migrateLine: pathGrantFlagsReplaceWords},
	11: {
		migrateLine:    namedKindsReplaceBareNouns,
		migrateJournal: harnessNoticesReplaceSubmittedProse,
		finalise:       addSessionMeta,
	},
}

var legacyGrantAccess = map[string]string{
	"read":  "r",
	"write": "rw",
	"exec":  "rx",
}

func pathGrantFlagsReplaceWords(line map[string]json.RawMessage) error {
	return with(line, func(event map[string]json.RawMessage) error {
		if string(event["kind"]) != `"`+string(pathgrant.Change)+`"` {
			return nil
		}

		raw, ok := event["state"]
		if !ok {
			return nil
		}

		var state struct {
			Grants []struct {
				Path   string `json:"path"`
				Access string `json:"access"`
			} `json:"grants"`
		}
		if err := json.Unmarshal(raw, &state); err != nil {
			return fmt.Errorf("the path grants could not be read: %w", err)
		}

		for index, grant := range state.Grants {
			flags, isWord := legacyGrantAccess[grant.Access]
			if !isWord {
				continue
			}
			state.Grants[index].Access = flags
		}

		restatedState, err := json.Marshal(state)
		if err != nil {
			return err
		}
		event["state"] = restatedState

		return nil
	})
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
	grantedCaps, err := caps.Parse(mode)
	if err != nil {
		return false
	}

	for flag := range strings.SplitSeq(caps.AllFlags, "") {
		swappedCaps, isNamed := caps.Named(flag)
		if !isNamed {
			continue
		}
		if notice, isSaid := caps.Notice(swappedCaps, grantedCaps); isSaid && notice == text {
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
			if string(event["kind"]) != `"startup"` {
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

	encodedEvent, err := json.Marshal(agent.Event{Kind: caps.ModeChange, Text: currentCaps.Flags()})
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
		if err == nil && hasEvent && event.Kind == legacyHarnessMessage && legacyEventFailed(lines[index]) &&
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

func harnessNoticesReplaceSubmittedProse(
	lines []map[string]json.RawMessage,
) ([]map[string]json.RawMessage, error) {
	migratedLines := make([]map[string]json.RawMessage, 0, len(lines))
	knownPaths := map[string]bool{}

	for index, line := range lines {
		event, hasEvent, err := eventOf(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", index+1, err)
		}

		if hasEvent && event.Kind == pathgrant.Change {
			for _, path := range grantedPaths(event) {
				knownPaths[path] = true
			}
		}

		if hasEvent && event.Kind == legacyHarnessMessage {
			continue
		}

		if !hasEvent || event.Kind != agent.UserMessageEvent || len(migratedLines) == 0 {
			migratedLines = append(migratedLines, line)
			continue
		}

		previousLine := migratedLines[len(migratedLines)-1]
		previousEvent, hasPreviousEvent, err := eventOf(previousLine)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", index, err)
		}
		if !hasPreviousEvent || !announcesItself(previousEvent.Kind) {
			migratedLines = append(migratedLines, line)
			continue
		}

		noticeEvent, isNamed := nameHarnessNotice(previousEvent, event.Text, knownPaths)
		if !isNamed {
			migratedLines = append(migratedLines, line)
			continue
		}

		encodedEvent, err := json.Marshal(noticeEvent)
		if err != nil {
			return nil, err
		}
		previousLine["event"] = encodedEvent
	}

	return migratedLines, nil
}

func announcesItself(kind agent.Kind) bool {
	return kind == caps.ModeChange || kind == pathgrant.Change
}

func nameHarnessNotice(event agent.Event, notice string, knownPaths map[string]bool) (agent.Event, bool) {
	for _, name := range noticeNames(event, knownPaths) {
		candidate := event
		candidate.Name = name
		if candidateNotice, isSaid := harnessNotice(candidate); isSaid && candidateNotice == notice {
			return candidate, true
		}
	}

	return event, false
}

func noticeNames(event agent.Event, knownPaths map[string]bool) []string {
	if event.Kind == caps.ModeChange {
		return strings.Split(caps.AllFlags, "")
	}

	return slices.Sorted(maps.Keys(knownPaths))
}

func harnessNotice(event agent.Event) (string, bool) {
	if event.Kind == caps.ModeChange {
		return caps.ModeNotice(event)
	}

	return pathgrant.Notice(event)
}

func grantedPaths(event agent.Event) []string {
	var state struct {
		Grants []struct {
			Path string `json:"path"`
		} `json:"grants"`
	}
	if err := json.Unmarshal(event.State, &state); err != nil {
		return nil
	}

	paths := make([]string, 0, len(state.Grants))
	for _, grant := range state.Grants {
		paths = append(paths, grant.Path)
	}

	return paths
}

const legacyHarnessMessage agent.Kind = "harness_message"

var renamedKinds = map[string]agent.Kind{
	"startup":      agent.StartupEvent,
	"interruption": agent.InterruptionEvent,
	"retrying":     agent.RetryingEvent,
	"failure":      agent.FailureEvent,
}

func namedKindsReplaceBareNouns(line map[string]json.RawMessage) error {
	return with(line, func(event map[string]json.RawMessage) error {
		var kind string
		if err := json.Unmarshal(event["kind"], &kind); err != nil {
			return fmt.Errorf("the event kind could not be read: %w", err)
		}

		if renamedKind, isRenamed := renamedKinds[kind]; isRenamed {
			kind = string(renamedKind)
			encodedKind, err := json.Marshal(kind)
			if err != nil {
				return err
			}
			event["kind"] = encodedKind
		}

		if err := dropEqualTotalBytes(event); err != nil {
			return err
		}

		if kind == string(caps.ModeChange) {
			if flags, hasFlags := event["text"]; hasFlags {
				event["state"] = flags
				delete(event, "text")
			}
		}

		text := ""
		if raw, hasText := event["text"]; hasText {
			if err := json.Unmarshal(raw, &text); err != nil {
				return fmt.Errorf("the event text could not be read: %w", err)
			}
		}

		switch {
		case kind == string(agent.InterruptionEvent) && text != "":
			cause, isKnown := interrupt.CauseSaying(text)
			if !isKnown {
				return nil
			}
			encodedCause, err := json.Marshal(string(cause))
			if err != nil {
				return err
			}
			event["name"] = encodedCause
			delete(event, "text")
		case kind == string(agent.UserMessageEvent) && text == turn.PokeMessage:
			encodedKind, err := json.Marshal(string(turn.HarnessPoke))
			if err != nil {
				return err
			}
			event["kind"] = encodedKind
			delete(event, "text")
		case kind == string(legacyHarnessMessage) && text == agent.SilentTurnNotice:
			encodedKind, err := json.Marshal(string(agent.SilentTurnEvent))
			if err != nil {
				return err
			}
			event["kind"] = encodedKind
			delete(event, "text")
			delete(event, "status")
		}

		return nil
	})
}

func dropEqualTotalBytes(event map[string]json.RawMessage) error {
	raw, hasStats := event["stats"]
	if !hasStats {
		return nil
	}

	var stats map[string]json.RawMessage
	if err := json.Unmarshal(raw, &stats); err != nil {
		return fmt.Errorf("the call measurements could not be read: %w", err)
	}
	if string(stats["total_bytes"]) != string(stats["bytes"]) {
		return nil
	}

	delete(stats, "total_bytes")
	encodedStats, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	event["stats"] = encodedStats

	return nil
}
