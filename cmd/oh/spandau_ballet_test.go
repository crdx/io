package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/output"
	shelltool "crdx.org/io/cmd/oh/shell"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/provider/anthropic"
	"crdx.org/io/provider/chat"
	"crdx.org/io/provider/codex"
	"crdx.org/io/session"
	"crdx.org/io/tool"
)

const sessionGoldenSystemPrompt = "You are a test assistant."

type sessionGoldenResponse struct {
	Events               []string          `toml:"event"`
	Lines                []string          `toml:"line"`
	Body                 string            `toml:"body"`
	Headers              map[string]string `toml:"headers"`
	Status               int               `toml:"status"`
	CancelAfterWireEvent int               `toml:"cancel-after-wire-event"`
	ResetAfterWireEvent  int               `toml:"reset-after-wire-event"`
	WaitForCancellation  bool              `toml:"wait-for-cancellation"`
}

type sessionGoldenTurn struct {
	Prompt                    string                  `toml:"prompt"`
	Responses                 []sessionGoldenResponse `toml:"response"`
	Timeout                   string                  `toml:"timeout"`
	IsCancelled               bool                    `toml:"is-cancelled"`
	CancelAfterReasoningDelta int                     `toml:"cancel-after-reasoning-delta"`
	CancelAfterReasoningEvent int                     `toml:"cancel-after-reasoning-event"`
	CancelAfterMessageDelta   int                     `toml:"cancel-after-message-delta"`
}

type sessionGoldenTool struct {
	Name          string   `toml:"name"`
	Outputs       []string `toml:"outputs"`
	StateKey      string   `toml:"state-key"`
	ShellWithheld bool     `toml:"shell-withheld"`
}

type sessionGoldenScenario struct {
	Name            string              `toml:"-"`
	Provider        string              `toml:"provider"`
	Model           string              `toml:"model"`
	Effort          string              `toml:"effort"`
	FirstTokenError string              `toml:"first-token-error"`
	Tools           []sessionGoldenTool `toml:"tool"`
	FirstTurn       sessionGoldenTurn   `toml:"first"`
	ResumeTurn      sessionGoldenTurn   `toml:"resume"`
}

func TestScenariosProduceCanonicalOutputs(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "scenarios", "*.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no session golden scenarios")
	}

	for _, path := range paths {
		scenario := readSessionGoldenScenario(t, path)
		t.Run(scenario.Name, func(t *testing.T) {
			for extension, got := range runSessionGoldenScenario(t, scenario) {
				compareScenarioGolden(t, scenario.Name+extension, got)
			}
		})
	}
}

func readSessionGoldenScenario(t *testing.T, path string) sessionGoldenScenario {
	t.Helper()

	var scenario sessionGoldenScenario
	metadata, err := toml.DecodeFile(path, &scenario)
	if err != nil {
		t.Fatal(err)
	}
	if undecodedKeys := metadata.Undecoded(); len(undecodedKeys) > 0 {
		t.Fatalf("%s: no such setting: %s", path, undecodedKeys[0])
	}

	scenario.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return scenario
}

func compareScenarioGolden(t *testing.T, name string, got string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", "output", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, got, want)
	}
}

type failingAnthropicTokenSource struct {
	failure error
}

func (self failingAnthropicTokenSource) Token() (string, error) {
	return "", self.failure
}

func newSessionGoldenAnthropicTokenSource(tokenError string) anthropic.TokenSource {
	if tokenError != "" {
		return failingAnthropicTokenSource{failure: errors.New(tokenError)}
	}
	return anthropic.Static("test-token")
}

func newSessionGoldenProvider(
	t *testing.T,
	scenario sessionGoldenScenario,
	endpoint string,
	tokenError string,
) agent.Provider {
	t.Helper()

	switch scenario.Provider {
	case "anthropic":
		tokens := newSessionGoldenAnthropicTokenSource(tokenError)
		client, err := anthropic.New(tokens, scenario.Model, scenario.Effort, 128_000)
		if err != nil {
			t.Fatal(err)
		}
		client.URL = endpoint
		return client
	case "codex":
		client, err := codex.New(codex.Static("test-token", "test-account"), scenario.Model, scenario.Effort)
		if err != nil {
			t.Fatal(err)
		}
		client.URL = endpoint
		return client
	case "chat":
		client, err := chat.New(endpoint, "test-token", scenario.Model, scenario.Effort, 128_000)
		if err != nil {
			t.Fatal(err)
		}
		return client
	default:
		t.Fatalf("unknown provider %q", scenario.Provider)
		return nil
	}
}

func newSessionGoldenTools(t *testing.T, specifications []sessionGoldenTool) []tool.Tool {
	t.Helper()

	tools := make([]tool.Tool, 0, len(specifications))
	for _, specification := range specifications {
		if specification.ShellWithheld {
			tools = append(tools, newSessionGoldenWithheldShell(t))
			continue
		}

		callCount := 0
		builder := tool.Implement[struct{}](
			tool.Definition{Name: specification.Name, Description: "A deterministic scenario tool."},
			func(struct{}) (string, string) { return specification.Name, "" },
		)
		if specification.StateKey != "" {
			builder = builder.State(specification.StateKey, func(state json.RawMessage) error {
				return json.Unmarshal(state, &callCount)
			})
		}
		tools = append(tools, builder.Run(func(context.Context, struct{}) (tool.ToolCallResult, error) {
			if callCount >= len(specification.Outputs) {
				return tool.ToolCallResult{}, fmt.Errorf("tool %s has no output for call %d", specification.Name, callCount+1)
			}

			output := specification.Outputs[callCount]
			callCount++
			state, err := json.Marshal(callCount)
			if err != nil {
				return tool.ToolCallResult{}, err
			}
			return tool.ToolCallResult{Output: output, State: state}, nil
		}))
	}
	return tools
}

func newSessionGoldenWithheldShell(t *testing.T) tool.Tool {
	t.Helper()

	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaceRoot.Close() })

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	mode := caps.NewMode(caps.Read)
	processes := sandbox.NewProcesses(false)
	t.Cleanup(func() { _, _ = processes.Disable() })

	return shelltool.New(workspace, t.TempDir(), t.TempDir(), shelltool.Paths{}, mode, files, processes)
}

func serveSessionGoldenResponse(
	writer http.ResponseWriter,
	request *http.Request,
	response sessionGoldenResponse,
	cancelSignals chan<- struct{},
) {
	writer.Header().Set("Content-Type", "text/event-stream")
	for name, value := range response.Headers {
		writer.Header().Set(name, value)
	}
	if response.Status != 0 && response.Status != http.StatusOK {
		writer.WriteHeader(response.Status)
		_, _ = fmt.Fprint(writer, response.Body)
		return
	}

	flusher, canFlush := writer.(http.Flusher)
	if !canFlush {
		panic("httptest response cannot flush")
	}

	for _, line := range response.Lines {
		_, _ = fmt.Fprintln(writer, line)
		flusher.Flush()
	}
	for i, payload := range response.Events {
		_, _ = fmt.Fprintf(writer, "data: %s\n\n", payload)
		flusher.Flush()
		switch i + 1 {
		case response.CancelAfterWireEvent:
			cancelSignals <- struct{}{}
			<-request.Context().Done()
			return
		case response.ResetAfterWireEvent:
			resetSessionGoldenConnection(writer)
			return
		}
	}
	if response.WaitForCancellation {
		<-request.Context().Done()
	}
}

func resetSessionGoldenConnection(writer http.ResponseWriter) {
	hijacker, canHijack := writer.(http.Hijacker)
	if !canHijack {
		panic("httptest response cannot be hijacked")
	}
	connection, _, err := hijacker.Hijack()
	if err != nil {
		panic(err)
	}
	_ = connection.Close()
}

func runSessionGoldenScenario(t *testing.T, scenario sessionGoldenScenario) map[string]string {
	t.Helper()

	responses := append(slices.Clone(scenario.FirstTurn.Responses), scenario.ResumeTurn.Responses...)
	cancelSignals := make(chan struct{}, len(responses))
	var requestCount atomic.Int32
	var requestMutex sync.Mutex
	var requestBodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestBody, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		requestMutex.Lock()
		requestBodies = append(requestBodies, requestBody)
		requestMutex.Unlock()

		requestIndex := int(requestCount.Add(1) - 1)
		if requestIndex >= len(responses) {
			http.Error(writer, "scenario has no response for this request", http.StatusInternalServerError)
			return
		}
		serveSessionGoldenResponse(writer, request, responses[requestIndex], cancelSignals)
	}))
	defer server.Close()

	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{
		Model:        scenario.Model,
		Provider:     scenario.Provider,
		Effort:       scenario.Effort,
		SystemPrompt: sessionGoldenSystemPrompt,
	})
	if err != nil {
		t.Fatal(err)
	}

	firstAssistant := agent.New(
		sessionGoldenSystemPrompt,
		newSessionGoldenProvider(t, scenario, server.URL, scenario.FirstTokenError),
		newSessionGoldenTools(t, scenario.Tools),
	)
	var firstScreenOutput bytes.Buffer
	firstHarness := &Harness{
		agent:  firstAssistant,
		screen: output.NewTerminalOfSize(&firstScreenOutput, replayColumns, replayLines),
		log:    log,
	}
	firstHarness.currentTurn = Turn{isRunning: true, painter: firstHarness.newPainter(true)}
	runSessionGoldenTurn(t, firstHarness, scenario.FirstTurn, cancelSignals)

	sessionName := log.Name()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}
	resumedAssistant := agent.New(
		storedSession.Meta.SystemPrompt,
		newSessionGoldenProvider(t, scenario, server.URL, ""),
		newSessionGoldenTools(t, scenario.Tools),
	)
	if err := resumedAssistant.RestoreState(storedSession.Events); err != nil {
		t.Fatal(err)
	}
	if err := resumedAssistant.Load(storedSession.Items); err != nil {
		t.Fatal(err)
	}

	log, err = store.Open(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}
	var screenOutput bytes.Buffer
	resumedHarness := &Harness{
		agent:         resumedAssistant,
		screen:        output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines),
		log:           log,
		events:        slices.Clone(storedSession.Events),
		flushBoundary: len(storedSession.Items),
	}
	resumedHarness.currentTurn = Turn{isRunning: true}
	resumedHarness.replay()
	requireSameVisibleScreen(
		t,
		"first interactive turn differs after resume",
		firstScreenOutput.String(),
		screenOutput.String(),
	)
	runSessionGoldenTurn(t, resumedHarness, scenario.ResumeTurn, cancelSignals)

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if got := int(requestCount.Load()); got != len(responses) {
		t.Errorf("provider received %d requests, want %d", got, len(responses))
	}

	transcriptPath := filepath.Join(directory, sessionName, "chat.md")
	transcript, err := os.ReadFile(transcriptPath) //nolint:gosec // the test's temporary bundle
	if err != nil {
		t.Fatal(err)
	}

	storedSession, err = store.Read(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}
	var replayOutput bytes.Buffer
	replayHarness := &Harness{
		agent:  resumedAssistant,
		screen: output.NewTerminalOfSize(&replayOutput, replayColumns, replayLines),
		events: storedSession.Events,
	}
	replayHarness.replay()

	requireSameVisibleScreen(
		t,
		"resumed live screen differs from its stored replay",
		screenOutput.String(),
		replayOutput.String(),
	)
	liveScreen := visibleScreen(t, screenOutput.String(), replayColumns)

	ansi := strings.TrimRight(visibleEscapes(screenOutput.String()), "\n") + "\n"
	settledScreen := strings.Join(liveScreen, "\n") + "\n"

	requestMutex.Lock()
	capturedRequestBodies := slices.Clone(requestBodies)
	requestMutex.Unlock()
	requests := canonicalProviderRequests(t, capturedRequestBodies)

	return map[string]string{
		".jsonl":          canonicalSessionJournal(t, directory, sessionName),
		".meta.json":      canonicalSessionMeta(t, directory, sessionName),
		".ansi":           ansi,
		".screen":         settledScreen,
		".transcript":     canonicalSessionTranscript(string(transcript), sessionName),
		".requests.jsonl": requests,
	}
}

func canonicalProviderRequests(t *testing.T, requestBodies [][]byte) string {
	t.Helper()

	var canonical bytes.Buffer
	for _, requestBody := range requestBodies {
		var request map[string]any
		if err := json.Unmarshal(requestBody, &request); err != nil {
			t.Fatal(err)
		}

		delete(request, "prompt_cache_key")
		encoded, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		canonical.Write(encoded)
		canonical.WriteByte('\n')
	}
	return canonical.String()
}

func requireSameVisibleScreen(t *testing.T, description string, firstOutput string, secondOutput string) {
	t.Helper()

	firstScreen := visibleScreen(t, firstOutput, replayColumns)
	secondScreen := visibleScreen(t, secondOutput, replayColumns)
	if !slices.Equal(firstScreen, secondScreen) {
		t.Errorf(
			"%s\nfirst:\n%s\nsecond:\n%s",
			description,
			strings.Join(firstScreen, "\n"),
			strings.Join(secondScreen, "\n"),
		)
	}
}

func runSessionGoldenTurn(
	t *testing.T,
	testHarness *Harness,
	turn sessionGoldenTurn,
	cancelSignals <-chan struct{},
) {
	t.Helper()

	var streamContext context.Context
	var cancel context.CancelFunc
	if turn.Timeout == "" {
		streamContext, cancel = context.WithCancel(t.Context())
	} else {
		timeout, err := time.ParseDuration(turn.Timeout)
		if err != nil {
			t.Fatal(err)
		}
		streamContext, cancel = context.WithTimeout(t.Context(), timeout)
	}
	defer cancel()
	testHarness.currentTurn.cancel = cancel
	editor := edit.NewInput(nil)
	interruptWithEscape := func() {
		if !testHarness.apply(editor, nil, key.Key{Code: key.Escape}) {
			t.Fatal("escape closed the harness")
		}
	}

	go func() {
		select {
		case <-cancelSignals:
			cancel()
		case <-streamContext.Done():
		}
	}()

	reasoningDeltas := 0
	reasoningEvents := 0
	messageDeltas := 0
	for update, streamError := range testHarness.agent.Stream(streamContext, turn.Prompt) {
		testHarness.takeTurn(TurnEvent{update: update, err: streamError})
		if update.Delta != nil {
			switch update.Delta.Kind {
			case agent.ModelReasoningEvent:
				reasoningDeltas++
				if reasoningDeltas == turn.CancelAfterReasoningDelta {
					interruptWithEscape()
				}
			case agent.ModelMessageEvent:
				messageDeltas++
				if messageDeltas == turn.CancelAfterMessageDelta {
					interruptWithEscape()
				}
			}
		}
		if update.Event != nil && update.Event.Kind == agent.ModelReasoningEvent {
			reasoningEvents++
			if reasoningEvents == turn.CancelAfterReasoningEvent {
				interruptWithEscape()
			}
		}
	}
	testHarness.currentTurn.isCancelled = turn.IsCancelled
	testHarness.finish()
}

func canonicalSessionMeta(t *testing.T, directory string, name string) string {
	t.Helper()

	encoded, err := os.ReadFile(filepath.Join(directory, name, "meta.json")) //nolint:gosec // the test's temporary bundle
	if err != nil {
		t.Fatal(err)
	}

	var meta map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &meta); err != nil {
		t.Fatal(err)
	}
	canonicalName, err := json.Marshal("brave-otter")
	if err != nil {
		t.Fatal(err)
	}
	canonicalTime, err := json.Marshal(transcriptTime)
	if err != nil {
		t.Fatal(err)
	}
	meta["name"] = canonicalName
	meta["started"] = canonicalTime
	meta["touched"] = canonicalTime

	canonical, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	return string(append(canonical, '\n'))
}

func canonicalSessionJournal(t *testing.T, directory string, name string) string {
	t.Helper()

	var canonical bytes.Buffer
	err := session.Records(directory, name, func(line session.Line) error {
		var event *agent.Event
		if line.Event != nil {
			canonicalEvent := *line.Event
			canonicalEvent.Took = 0
			event = &canonicalEvent
		}

		record := struct {
			Kind    session.Kind    `json:"kind"`
			Version int             `json:"version,omitempty"`
			Meta    json.RawMessage `json:"meta,omitempty"`
			Event   *agent.Event    `json:"event,omitempty"`
			Payload json.RawMessage `json:"payload,omitempty"`
		}{
			Kind:    line.Kind,
			Version: line.Version,
			Meta:    line.Meta,
			Event:   event,
			Payload: line.Payload,
		}

		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		canonical.Write(encoded)
		canonical.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return canonical.String()
}

var (
	transcriptStartedPattern  = regexp.MustCompile(`(?m)^- \*\*Started:\*\* ` + "`[^`]+`$")
	transcriptEventPattern    = regexp.MustCompile(`(?m)^> [^\n]+$`)
	transcriptDurationPattern = regexp.MustCompile(`(?m)^- \*\*Duration:\*\* ` + "`[^`]+`$")
)

func canonicalSessionTranscript(transcript string, sessionName string) string {
	canonical := strings.ReplaceAll(transcript, sessionName, "brave-otter")
	canonical = transcriptStartedPattern.ReplaceAllString(
		canonical,
		"- **Started:** `"+transcriptTime.Format(time.RFC3339Nano)+"`",
	)
	canonical = transcriptDurationPattern.ReplaceAllString(canonical, "- **Duration:** `0s`")

	eventIndex := 0
	return transcriptEventPattern.ReplaceAllStringFunc(canonical, func(string) string {
		when := transcriptTime.Add(time.Duration(eventIndex) * time.Second)
		eventIndex++
		return "> " + when.Format(time.RFC3339Nano)
	})
}
