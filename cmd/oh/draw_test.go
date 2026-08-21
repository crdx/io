package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path"
	"runtime"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/status"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/pathutil"
	"crdx.org/io/tool"
	"crdx.org/io/tool/middleware/truncate"
)

func TestWhatWasAskedIsRenderedIntoTheConversation(t *testing.T) {
	for _, isLive := range []bool{true, false} {
		var screenOutput bytes.Buffer

		painter := &painter{screen: output.New(&screenOutput), isLive: isLive}
		painter.draw(agent.Event{Kind: agent.Prompt, Text: "**weather**\n\n- today"})

		plain := style.Plain(screenOutput.String())
		if !strings.Contains(plain, " weather\n \n • today") {
			t.Errorf("isLive=%v: expected the question's markdown to be rendered, got %q", isLive, plain)
		}

		if strings.Contains(plain, "> ") || strings.Contains(plain, "**") {
			t.Errorf("isLive=%v: expected no literal prompt or markdown markers, got %q", isLive, plain)
		}

		if !strings.HasSuffix(plain, "\n") {
			t.Errorf("isLive=%v: expected the submitted message to finish its line immediately, got %q", isLive, plain)
		}
	}
}

func TestASubmittedMessageHasBackgroundRowsAboveAndBelowIt(t *testing.T) {
	got := style.Plain(renderSubmittedMessage("hello", 8))
	want := "        \n hello  \n        "

	if got != want {
		t.Errorf("submitted message was %q, want %q", got, want)
	}
}

func TestReplayingACallThatWasNeverAnsweredLeavesNothingRunning(t *testing.T) {
	blocksBefore := blocksStillRunning(t)

	var screenOutput bytes.Buffer

	testConversation := &conversation{
		assistant: agent.New("", quietProvider{}, nil),
		screen:    output.New(&screenOutput),
	}

	testConversation.restore(&store.Session{
		Events: []agent.Event{
			{Kind: agent.Prompt, Text: "read them both"},
			{Kind: agent.Call, ID: "1", Name: "read", Rendering: agent.Rendering{Subject: "one.go"}},
			{Kind: agent.Call, ID: "2", Name: "read", Rendering: agent.Rendering{Subject: "two.go"}},
			{Kind: agent.Result, ID: "1", Name: "read", Text: "package one"},
		},
	})

	if got := blocksStillRunning(t); got > blocksBefore {
		t.Errorf("expected the block to have been closed, got %d redrawing where there were %d", got, blocksBefore)
	}
}

func TestRestoringAConversationRestoresStateBeforeReturning(t *testing.T) {
	var restored string
	definedTool := tool.Implement(
		tool.Definition{Name: "stateful", Description: "", Schema: tool.Schema{}},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) { return "", nil })
	statefulTool := tool.State(definedTool, "test_state", func(state json.RawMessage) error {
		restored = string(state)
		return nil
	})

	var screenOutput bytes.Buffer
	self := &conversation{
		assistant: agent.New("", quietProvider{}, []tool.Tool{statefulTool}),
		screen:    output.New(&screenOutput),
	}
	self.restore(&store.Session{Events: []agent.Event{{
		Kind:  agent.StateEvent,
		Name:  "test_state",
		State: json.RawMessage(`{"answer":42}`),
	}}})

	if restored != `{"answer":42}` {
		t.Errorf("got restored state %q", restored)
	}
}

func blocksStillRunning(t *testing.T) int {
	t.Helper()

	stacks := make([]byte, 1<<16)
	for {
		if wrote := runtime.Stack(stacks, true); wrote < len(stacks) {
			stacks = stacks[:wrote]
			break
		}

		stacks = make([]byte, 2*len(stacks))
	}

	return strings.Count(string(stacks), "status.(*Block).run(")
}

func TestReplayingSaysTheWholeConversationAgain(t *testing.T) {
	var screenOutput bytes.Buffer

	testConversation := &conversation{
		assistant: agent.New("", quietProvider{}, nil),
		screen:    output.New(&screenOutput),
	}

	testConversation.transcript = []agent.Event{
		{Kind: agent.Prompt, Text: "what is the weather"},
		{Kind: agent.Text, Text: "it is raining"},
		{Kind: agent.Notice, Text: "cancelled"},
	}

	testConversation.replay()

	for _, want := range []string{"what is the weather", "it is raining", "cancelled"} {
		if !strings.Contains(screenOutput.String(), want) {
			t.Errorf("expected %q to be drawn again, got %q", want, screenOutput.String())
		}
	}
}

func TestAReadOfASkillIsDrawnAsTheSkill(t *testing.T) {
	const skillPath = "/skills/golang/SKILL.md"

	tests := map[string]struct {
		tool    string
		path    string
		want    string
		painted string
	}{
		"a read of a skill": {
			tool: "read", path: skillPath,
			want: "load " + skillPath, painted: style.Skill("golang"),
		},
		"a read of a file": {
			tool: "read", path: "cmd/oh/draw.go",
			want: "read cmd/oh/draw.go", painted: style.Subject("draw.go"),
		},
		"another tool": {tool: "grep", path: skillPath, want: "grep " + skillPath},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var screenOutput bytes.Buffer
			callPainter := &painter{screen: output.New(&screenOutput)}

			callPainter.draw(agent.Event{
				Kind: agent.Call, ID: "1", Name: test.tool,
				Rendering: agent.Rendering{
					Subject:   test.path,
					Highlight: tool.Highlight{Kind: tool.HighlightFocus, Value: path.Base(test.path)},
				},
			})
			callPainter.close(status.Done)

			if plain := style.Plain(screenOutput.String()); !strings.Contains(plain, test.want) {
				t.Errorf("got %q, want %q", plain, test.want)
			}
			if test.painted != "" && !strings.Contains(screenOutput.String(), test.painted) {
				t.Errorf("got %q, want %q painted", screenOutput.String(), test.painted)
			}
		})
	}
}

func TestTheFileASkillIsKeptInIsNotStoodOut(t *testing.T) {
	var screenOutput bytes.Buffer
	callPainter := &painter{screen: output.New(&screenOutput)}

	callPainter.draw(agent.Event{
		Kind: agent.Call, ID: "1", Name: "read",
		Rendering: agent.Rendering{
			Subject:   "/skills/golang/SKILL.md",
			Highlight: tool.Highlight{Kind: tool.HighlightFocus, Value: "SKILL.md"},
		},
	})
	callPainter.close(status.Done)

	if strings.Contains(screenOutput.String(), style.Subject("SKILL.md")) {
		t.Errorf("got %q, want the file left dim", screenOutput.String())
	}
}

func TestWhetherACallChangedAnythingComesFromTheToolOfTheMoment(t *testing.T) {
	var screenOutput bytes.Buffer
	self := &conversation{
		assistant: agent.New("", quietProvider{}, []tool.Tool{slowTool("write")}),
		screen:    output.New(&screenOutput),
	}
	callPainter := self.newPainter(false)

	callPainter.draw(agent.Event{
		Kind: agent.Call, ID: "1", Name: "write", Arguments: `{"path":"one.go"}`,
		Rendering: agent.Rendering{ReadOnly: true},
	})
	callPainter.close(status.Done)

	if want := style.Change("write"); !strings.Contains(screenOutput.String(), want) {
		t.Errorf("got %q, want %q", screenOutput.String(), want)
	}
}

func TestACallToAToolThatIsGoneKeepsWhatWasRecorded(t *testing.T) {
	var screenOutput bytes.Buffer
	self := &conversation{
		assistant: agent.New("", quietProvider{}, nil),
		screen:    output.New(&screenOutput),
	}
	callPainter := self.newPainter(false)

	callPainter.draw(agent.Event{
		Kind: agent.Call, ID: "1", Name: "gone",
		Rendering: agent.Rendering{Subject: "one.go", ReadOnly: true},
	})
	callPainter.close(status.Done)

	if want := style.Call("gone"); !strings.Contains(screenOutput.String(), want) {
		t.Errorf("got %q, want %q", screenOutput.String(), want)
	}
}

func TestAHarnessNoticeIsDrawnTheSameLiveAndReplayed(t *testing.T) {
	const notice = "Background processes killed (tmux: server → bash → sleep)"

	var live strings.Builder
	self := &conversation{
		assistant: agent.New("", quietProvider{}, nil),
		screen:    output.NewTerminalOfSize(&live, 80, 24),
		log:       testLog(t),
	}

	call := agent.Event{Kind: agent.Call, ID: "1", Name: "read", Rendering: agent.Rendering{Subject: "one.go"}}

	self.turn = turn{isRunning: true, painter: self.newPainter(true)}
	self.transcript = appendTranscript(self.transcript, call)
	self.turn.painter.draw(call)

	self.notifyStopped(notice)

	self.turn.painter.close(status.Cancelled)
	self.turn = turn{}
	self.screen.End()

	var replayOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&replayOutput, 80, 24)
	self.replay()

	if visibleScreen(t, live.String(), 80) == nil ||
		strings.Join(visibleScreen(t, live.String(), 80), "\n") !=
			strings.Join(visibleScreen(t, replayOutput.String(), 80), "\n") {
		t.Errorf(
			"a notice said live leaves a different screen from one replayed\nlive:\n%s\nreplayed:\n%s",
			strings.Join(visibleScreen(t, live.String(), 80), "\n"),
			strings.Join(visibleScreen(t, replayOutput.String(), 80), "\n"),
		)
	}
}

func TestTheWholeConversationIsDrawnTheSameLiveAndReplayed(t *testing.T) {
	tools := []tool.Tool{slowTool("read")}

	var live bytes.Buffer
	self := &conversation{
		assistant: agent.New("", quietProvider{}, tools),
		screen:    output.New(&live),
		log:       testLog(t),
	}
	livePainter := self.newPainter(true)

	events := []agent.Event{
		{Kind: agent.Prompt, Text: "**Check** this"},
		{Kind: agent.Reasoning, Text: "**Reading**\nLooking "},
		{Kind: agent.Reasoning, Text: "at the file. Need care."},
		{Kind: agent.Text, Text: "The **first** "},
		{Kind: agent.Text, Text: "answer.\n"},
		{Kind: agent.Call, ID: "1", Name: "read", Arguments: `{"path":"one.go"}`, Rendering: agent.Rendering{Subject: "old"}},
		{Kind: agent.Call, ID: "2", Name: "gone", Rendering: agent.Rendering{Subject: "two.go"}},
		{Kind: agent.Result, ID: "1", Name: "read", Took: 2 * time.Second},
		{Kind: agent.Result, ID: "2", Name: "gone", Failed: true, Took: 3 * time.Second},
		{Kind: agent.Text, Text: "Done."},
	}

	for _, event := range events {
		self.transcript = appendTranscript(self.transcript, event)
		livePainter.draw(event)
	}
	self.notifyStopped("Background processes killed (bash → sleep)")

	unansweredCall := agent.Event{Kind: agent.Call, ID: "3", Name: "read", Rendering: agent.Rendering{Subject: "left.go"}}
	self.transcript = appendTranscript(self.transcript, unansweredCall)
	livePainter.draw(unansweredCall)
	livePainter.close(status.Cancelled)
	self.screen.End()

	var replayOutput bytes.Buffer
	self.screen = output.New(&replayOutput)
	self.replay()

	plain := style.Plain(live.String())

	for _, want := range []string{"Check", "Looking at the file.", "Need care.", "first answer.", "one.go", "Done.", "Background processes killed"} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected %q on the screen, got %q", want, plain)
		}
	}
	if !strings.Contains(plain, "Need care.\n\nThe first") {
		t.Errorf("expected a blank line between reasoning and the answer, got %q", plain)
	}

	if live.String() != replayOutput.String() {
		t.Errorf("live conversation %q differs from replayed conversation %q", live.String(), replayOutput.String())
	}
}

func TestAThoughtHasMarkdownStripped(t *testing.T) {
	rows := renderReasoning("## **Checking** `one.go`", 40)
	for i := range rows {
		rows[i] = style.Plain(rows[i])
	}

	if got := strings.Join(rows, "\n"); got != "Checking one.go" {
		t.Errorf("got reasoning %q with markdown stripped", got)
	}
}

func TestAThoughtRunsDirectlyIntoAToolCall(t *testing.T) {
	var screenOutput bytes.Buffer
	callPainter := &painter{screen: output.New(&screenOutput)}

	callPainter.draw(agent.Event{Kind: agent.Reasoning, Text: "checking"})
	callPainter.draw(agent.Event{Kind: agent.Call, ID: "1", Name: "read", Rendering: agent.Rendering{Subject: "one.go"}})
	callPainter.close(status.Cancelled)

	plain := style.Plain(screenOutput.String())
	if !strings.Contains(plain, "checking\nread one.go") {
		t.Errorf("expected no blank line between reasoning and the tool call, got %q", plain)
	}
}

func TestAThoughtWrapsAtWordBoundaries(t *testing.T) {
	rows := renderReasoning("one two three four", 9)
	for i := range rows {
		rows[i] = style.Plain(rows[i])
	}

	if got := strings.Join(rows, "\n"); got != "one two\nthree\nfour" {
		t.Errorf("got %q, want reasoning wrapped between words", got)
	}
}

func TestAStoredCallIsShownTheWayItsToolShowsItNow(t *testing.T) {
	var screenOutput bytes.Buffer

	current := truncate.Tool(tool.Focus(slowTool("read"), func(tool.Call) string { return "one.go" }))
	testConversation := &conversation{
		assistant: agent.New("", quietProvider{}, []tool.Tool{current}),
		screen:    output.New(&screenOutput),
	}

	testConversation.transcript = []agent.Event{{
		Kind:      agent.Call,
		ID:        "1",
		Name:      "read",
		Arguments: `{"path":"cmd/oh/one.go"}`,
		Rendering: agent.Rendering{Subject: "one.go:1-400"},
	}}

	testConversation.replay()

	if strings.Contains(screenOutput.String(), "one.go:1-400") {
		t.Errorf("expected the stored rendering to be redrawn, got %q", screenOutput.String())
	}

	want := style.Subtle("cmd/oh/") + style.Subject("one.go")
	if !strings.Contains(screenOutput.String(), want) {
		t.Errorf("expected the stored call to take the tool's current focus, got %q", screenOutput.String())
	}
}

func TestACallWhoseToolIsGoneKeepsWhatItLookedLike(t *testing.T) {
	var screenOutput bytes.Buffer

	testConversation := &conversation{
		assistant: agent.New("", quietProvider{}, nil),
		screen:    output.New(&screenOutput),
	}

	testConversation.transcript = []agent.Event{{
		Kind:      agent.Call,
		ID:        "1",
		Name:      "divine",
		Arguments: `{"path":"one.go"}`,
		Rendering: agent.Rendering{Subject: "one.go:1-400"},
	}}

	testConversation.replay()

	if !strings.Contains(screenOutput.String(), "one.go:1-400") {
		t.Errorf("expected what it looked like at the time, got %q", screenOutput.String())
	}
}

type quietProvider struct{}

func (quietProvider) Configure(string, []tool.Definition) {}
func (quietProvider) AddUserMessage(string)               {}
func (quietProvider) AddToolResults([]agent.ToolResult)   {}
func (quietProvider) Dump() []json.RawMessage             { return nil }
func (quietProvider) Load([]json.RawMessage)              {}

func (quietProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	return agent.Reply{}, nil
}

func testConversation(t *testing.T, screenOutput *bytes.Buffer) *conversation {
	t.Helper()

	log, err := store.Create(t.TempDir(), store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = log.Close() })

	backend := quietProvider{}

	return &conversation{
		assistant:          agent.New("", backend, nil),
		screen:             output.New(screenOutput),
		log:                log,
		mode:               NewMode(capRead | capWrite),
		getOnWithItMessage: defaultGetOnWithItMessage,
	}
}

func completeTurn(self *conversation) {
	self.start("are you there")

	for report := range self.turn.events {
		self.take(report)
	}

	self.finish()
}

func TestATurnThatFinishedByItselfIsNotCalledCancelled(t *testing.T) {
	var screenOutput bytes.Buffer

	completeTurn(testConversation(t, &screenOutput))

	if strings.Contains(screenOutput.String(), "cancelled") {
		t.Errorf("expected a finished turn to say nothing about cancelling, got %q", screenOutput.String())
	}
}

func TestACompletedTurnSendsADesktopNotification(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	notifications := 0
	self.notifyTurnFinished = func() { notifications++ }

	completeTurn(self)

	if notifications != 1 {
		t.Errorf("got %d notifications, want one", notifications)
	}
}

func TestACompletedTurnDoesNotNotifyWhileTheTerminalIsFocused(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.terminalFocused = true
	notifications := 0
	self.notifyTurnFinished = func() { notifications++ }

	completeTurn(self)

	if notifications != 0 {
		t.Errorf("got %d notifications, want none", notifications)
	}
}

func TestAnInterruptedTurnDoesNotSendADesktopNotification(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	notifications := 0
	self.notifyTurnFinished = func() { notifications++ }

	self.start("are you there")
	self.turn.isCancelled = true
	self.turn.stop()

	for report := range self.turn.events {
		self.take(report)
	}

	self.finish()

	if notifications != 0 {
		t.Errorf("got %d notifications, want none", notifications)
	}
}

func TestAStoppedTurnIsNotAnnouncedInTheScrollback(t *testing.T) {
	var screenOutput bytes.Buffer

	self := testConversation(t, &screenOutput)

	self.start("are you there")
	self.turn.isCancelled = true
	self.turn.stop()

	for report := range self.turn.events {
		self.take(report)
	}

	self.finish()

	plain := style.Plain(screenOutput.String())
	if strings.Contains(plain, "Interrupted") {
		t.Errorf("expected the interruption to stay out of the scrollback, got %q", plain)
	}

	if strings.Contains(screenOutput.String(), "context canceled") {
		t.Errorf("expected the stop to be reported as a stop, got %q", screenOutput.String())
	}
}

func TestAShellCallIsDrawnAsAShellPrompt(t *testing.T) {
	var screenOutput bytes.Buffer
	callPainter := &painter{screen: output.New(&screenOutput), shell: "bash"}

	callPainter.draw(agent.Event{Kind: agent.Call, ID: "1", Name: "bash", Rendering: agent.Rendering{Subject: "echo hello"}})
	callPainter.close(status.Done)

	plain := style.Plain(screenOutput.String())
	if !strings.Contains(plain, "$ echo hello") {
		t.Errorf("got %q, want a shell prompt", plain)
	}
	if strings.Contains(plain, "bash echo hello") {
		t.Errorf("got %q, want no tool name", plain)
	}
}

func TestATurnThatLeftACallUnansweredIsClosedByWhatIsAskedNext(t *testing.T) {
	var screenOutput bytes.Buffer

	callPainter := &painter{screen: output.New(&screenOutput)}

	callPainter.draw(agent.Event{Kind: agent.Call, ID: "1", Name: "read", Rendering: agent.Rendering{Subject: "one.go"}})
	callPainter.draw(agent.Event{Kind: agent.Prompt, Text: "never mind"})

	if callPainter.toolBlock != nil {
		t.Fatal("expected the block to be closed by the next thing asked")
	}

	callPainter.draw(agent.Event{Kind: agent.Call, ID: "2", Name: "read", Rendering: agent.Rendering{Subject: "two.go"}})

	if got := callPainter.rows["2"]; got != 0 {
		t.Errorf("expected the call to open a block of its own, got row %d", got)
	}

	callPainter.close(status.Cancelled)
}

func TestARedrawDuringATurnHandsTheOpenBlockToTheTurn(t *testing.T) {
	blocksBefore := blocksStillRunning(t)

	var screenOutput bytes.Buffer

	testConversation := &conversation{
		assistant: agent.New("", quietProvider{}, nil),
		screen:    output.New(&screenOutput),
	}

	testConversation.turn = turn{isRunning: true, painter: testConversation.newPainter(true)}

	testConversation.transcript = []agent.Event{
		{Kind: agent.Prompt, Text: "read it"},
		{Kind: agent.Call, ID: "1", Name: "read", Rendering: agent.Rendering{Subject: "one.go"}},
	}

	previousPainter := testConversation.turn.painter

	testConversation.redraw()

	if testConversation.turn.painter == previousPainter {
		t.Fatal("expected the turn to be given the painter that drew the replay")
	}

	if testConversation.turn.painter.toolBlock == nil {
		t.Fatal("expected the unanswered call to be on a block that is open again")
	}

	testConversation.turn.painter.draw(agent.Event{Kind: agent.Result, ID: "1", Name: "read", Took: time.Second})

	if testConversation.turn.painter.toolBlock != nil {
		t.Error("expected the block to close once every call had reported")
	}

	if got := blocksStillRunning(t); got > blocksBefore {
		t.Errorf("expected nothing left redrawing, got %d blocks where there were %d", got, blocksBefore)
	}
}

func TestAnAnswerStreamedIsTheSameAsTheAnswerReplayed(t *testing.T) {
	const answer = "# Findings\n\nThe **first** thing is `read`.\n\n- one\n- two\n"

	var live bytes.Buffer

	livePainter := &painter{screen: output.New(&live), isLive: true}

	for _, delta := range deltas(answer, 10) {
		livePainter.draw(agent.Event{Kind: agent.Text, Text: delta})
	}

	livePainter.screen.End()

	var replayOutput bytes.Buffer

	replayPainter := &painter{screen: output.New(&replayOutput)}
	replayPainter.draw(agent.Event{Kind: agent.Text, Text: answer})
	replayPainter.screen.End()

	plain := style.Plain(live.String())

	for _, want := range []string{"Findings", "first thing is", "• one", "• two"} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected %q drawn, got %q", want, plain)
		}
	}

	if live.String() != replayOutput.String() {
		t.Errorf("streamed %q, replayed %q", live.String(), replayOutput.String())
	}

	if strings.Contains(live.String(), "**") {
		t.Errorf("expected the markdown to be drawn rather than shown, got %q", live.String())
	}
}

func deltas(text string, count int) []string {
	var pieces []string

	size := (len(text) + count - 1) / count

	for at := 0; at < len(text); at += size {
		pieces = append(pieces, text[at:min(at+size, len(text))])
	}

	return pieces
}

func TestWorkspacePrefixIsOmittedFromRenderedCallPaths(t *testing.T) {
	const workspaceDir = "/home/alice/project"

	t.Setenv("HOME", "/home/alice")

	current := tool.FocusPath(tool.Implement(
		tool.Definition{
			Name:        "read",
			Description: "",
			Schema:      tool.Schema{tool.String("path", "file")},
		},
		func(args fakeArgs) (string, string) { return args.Path, pathutil.Shorten(args.Path) },
	).Plain(func(context.Context, fakeArgs) (string, error) { return "", nil }))
	testConversation := &conversation{
		assistant:    agent.New("", quietProvider{}, []tool.Tool{current}),
		workspaceDir: workspaceDir,
	}

	shown := testConversation.newPainter(false).describe(agent.Event{
		Name:      "read",
		Arguments: `{"path":"/home/alice/project/cmd/oh/draw.go"}`,
	})

	wantHighlight := tool.Highlight{Kind: tool.HighlightFocus, Value: "draw.go"}
	if shown.Highlight != wantHighlight {
		t.Fatalf("unexpected highlight %#v", shown.Highlight)
	}
	if shown.Subject != "cmd/oh/draw.go" || shown.Note != "cmd/oh/draw.go" {
		t.Errorf("got rendering %q and detail %q, want the workspace prefixes omitted", shown.Subject, shown.Note)
	}
}

func TestPathPrefixesAreShortened(t *testing.T) {
	const workspaceDir = "/home/alice/project"

	t.Setenv("HOME", "/home/alice")

	callPainter := &painter{workspaceDir: workspaceDir}
	tests := map[string]string{
		workspaceDir:                          "",
		"~/project":                           "",
		workspaceDir + " **/*.go":             "**/*.go",
		"~/project **/*.go":                   "**/*.go",
		workspaceDir + "/cmd/oh/draw.go":      "cmd/oh/draw.go",
		"~/project/cmd/oh/draw.go":            "cmd/oh/draw.go",
		"/home/alice/other.go":                "~/other.go",
		"/home/alice/projectile/other.go":     "~/projectile/other.go",
		"~/projectile/cmd/oh/draw.go **/*.go": "~/projectile/cmd/oh/draw.go **/*.go",
	}

	for value, want := range tests {
		if got := callPainter.shortenPathPrefix(value); got != want {
			t.Errorf("shortenPathPrefix(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestRecordedCallPathsAreShortenedWithTheSameFunction(t *testing.T) {
	t.Setenv("HOME", "/home/alice")

	callPainter := &painter{workspaceDir: "/home/alice/project"}
	shown := callPainter.describe(agent.Event{
		Name: "removed",
		Rendering: agent.Rendering{
			Subject: "/home/alice/project/file.go",
			Note:    "/home/alice/reference/file.go",
		},
	})

	if shown.Subject != "file.go" || shown.Note != "~/reference/file.go" {
		t.Errorf("got subject %q and qualifier %q, want both path prefixes shortened", shown.Subject, shown.Note)
	}
}

func TestARefusedCallIsDescribedAgainRatherThanFromTheRecord(t *testing.T) {
	refusing := tool.Implement(
		tool.Definition{
			Name:        "shout",
			Description: "",
			Schema:      tool.Schema{tool.String("message", "what to shout")},
		},
		func(struct{}) (string, string) { return "", "" },
	).Validate(func(struct{}) error {
		return errors.New("not in the mood")
	}).Plain(func(context.Context, struct{}) (string, error) { return "", nil })

	testConversation := &conversation{
		assistant: agent.New("", quietProvider{}, []tool.Tool{refusing}),
	}

	shown := testConversation.newPainter(false).describe(agent.Event{
		Name:      "shout",
		Arguments: `{"message":"oi"}`,
		Rendering: agent.Rendering{Subject: ""}, // as a session saved before the call could be described recorded it
	})

	if shown.Note != "" || shown.Highlight != (tool.Highlight{}) {
		t.Fatalf("unexpected call description %q, %#v", shown.Note, shown.Highlight)
	}
	if shown.Subject != "oi" {
		t.Errorf("got %q, want the arguments described again from the record", shown.Subject)
	}
}

func testLog(t *testing.T) *store.Writer {
	t.Helper()

	log, err := store.Create(t.TempDir(), store.Meta{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = log.Close() })

	return log
}
