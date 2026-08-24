package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util/pathutil"
	"crdx.org/io/tool"
	"crdx.org/io/tool/middleware/truncate"
)

func TestWhatWasAskedIsRenderedIntoTheConversation(t *testing.T) {
	for _, isLive := range []bool{true, false} {
		var screenOutput bytes.Buffer

		painter := &Painter{screen: output.New(&screenOutput), isRunning: isLive}
		painter.drawEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "**weather**\n\n- today"})

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

	testConversation := &Harness{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&screenOutput),
		recorder: recordSession(testLog(t)),
	}

	testConversation.restore(&store.Session{
		Events: []agent.Event{
			{Kind: agent.UserMessageEvent, Text: "read them both"},
			{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}},
			{Kind: agent.ToolCallRequestEvent, ID: "2", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "two.go"}},
			{Kind: agent.ToolCallResultEvent, ID: "1", Name: "read", Text: "package one"},
		},
	})

	if got := blocksStillRunning(t); got > blocksBefore {
		t.Errorf("expected the block to have been closed, got %d redrawing where there were %d", got, blocksBefore)
	}
}

func TestRestoringAConversationClearsTheTerminalBeforeReplaying(t *testing.T) {
	var screenOutput bytes.Buffer
	self := &Harness{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.NewTerminalOfSize(&screenOutput, 80, 24),
		recorder: recordSession(testLog(t)),
	}

	self.restore(&store.Session{Events: []agent.Event{{
		Kind: agent.UserMessageEvent,
		Text: "restored conversation",
	}}})

	const clearTerminal = "\x1b[H\x1b[2J\x1b[3J"
	if !strings.HasPrefix(screenOutput.String(), clearTerminal) {
		t.Errorf("expected the terminal to be cleared before replay, got %q", screenOutput.String())
	}
	if !strings.Contains(style.Plain(screenOutput.String()), "restored conversation") {
		t.Errorf("expected the conversation to be replayed after clearing, got %q", screenOutput.String())
	}
}

func TestRestoringAConversationRestoresStateBeforeReturning(t *testing.T) {
	var restored string
	statefulTool := tool.Implement(
		tool.Definition{Name: "stateful", Description: "", Schema: tool.Schema{}},
		func(struct{}) (string, string) { return "", "" },
	).State("test_state", func(state json.RawMessage) error {
		restored = string(state)
		return nil
	}).Plain(func(context.Context, struct{}) (string, error) { return "", nil })

	var screenOutput bytes.Buffer
	self := &Harness{
		agent:    agent.New("", quietProvider{}, []tool.Tool{statefulTool}),
		screen:   output.New(&screenOutput),
		recorder: recordSession(testLog(t)),
	}
	self.restore(&store.Session{Events: []agent.Event{{
		Kind:  agent.StateChangeEvent,
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

	return strings.Count(string(stacks), "dynamic.(*Block).run(")
}

func TestReplayingSaysTheWholeConversationAgain(t *testing.T) {
	var screenOutput bytes.Buffer

	testConversation := &Harness{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&screenOutput),
	}

	testConversation.events = []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "what is the weather"},
		{Kind: agent.ModelMessageEvent, Text: "it is raining"},
		{Kind: agent.HarnessMessageEvent, Text: "cancelled"},
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
			tool:    "read",
			path:    skillPath,
			want:    "load " + skillPath,
			painted: style.Skill("golang"),
		},
		"a read of a file": {
			tool:    "read",
			path:    "cmd/oh/draw.go",
			want:    "read cmd/oh/draw.go",
			painted: style.Subject("draw.go"),
		},
		"another tool": {tool: "grep", path: skillPath, want: "grep " + skillPath},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var screenOutput bytes.Buffer
			callPainter := &Painter{screen: output.New(&screenOutput)}

			callPainter.drawEvent(agent.Event{
				Kind: agent.ToolCallRequestEvent,
				ID:   "1",
				Name: test.tool,
				FallbackRendering: agent.FallbackRendering{
					Subject:  test.path,
					Emphasis: tool.Emphasis{Kind: tool.EmphasisFocus, Value: path.Base(test.path)},
				},
			})
			callPainter.close(dynamic.Done)

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
	callPainter := &Painter{screen: output.New(&screenOutput)}

	callPainter.drawEvent(agent.Event{
		Kind: agent.ToolCallRequestEvent,
		ID:   "1",
		Name: "read",
		FallbackRendering: agent.FallbackRendering{
			Subject:  "/skills/golang/SKILL.md",
			Emphasis: tool.Emphasis{Kind: tool.EmphasisFocus, Value: "SKILL.md"},
		},
	})
	callPainter.close(dynamic.Done)

	if strings.Contains(screenOutput.String(), style.Subject("SKILL.md")) {
		t.Errorf("got %q, want the file left dim", screenOutput.String())
	}
}

func TestWhetherACallChangedAnythingComesFromTheToolOfTheMoment(t *testing.T) {
	var screenOutput bytes.Buffer
	self := &Harness{
		agent:  agent.New("", quietProvider{}, []tool.Tool{slowTool("write")}),
		screen: output.New(&screenOutput),
	}
	callPainter := self.newPainter(false)

	callPainter.drawEvent(agent.Event{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "write",
		Arguments:         `{"path":"one.go"}`,
		FallbackRendering: agent.FallbackRendering{ReadOnly: true},
	})
	callPainter.close(dynamic.Done)

	if want := style.Change("write"); !strings.Contains(screenOutput.String(), want) {
		t.Errorf("got %q, want %q", screenOutput.String(), want)
	}
}

func TestACallToAToolThatIsGoneKeepsWhatWasRecorded(t *testing.T) {
	var screenOutput bytes.Buffer
	self := &Harness{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&screenOutput),
	}
	callPainter := self.newPainter(false)

	callPainter.drawEvent(agent.Event{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "gone",
		FallbackRendering: agent.FallbackRendering{Subject: "one.go", ReadOnly: true},
	})
	callPainter.close(dynamic.Done)

	if want := style.Call("gone"); !strings.Contains(screenOutput.String(), want) {
		t.Errorf("got %q, want %q", screenOutput.String(), want)
	}
}

func TestAHarnessNoticeIsDrawnTheSameLiveAndReplayed(t *testing.T) {
	const notice = "Background processes killed (tmux: server → bash → sleep)"

	var live strings.Builder
	self := &Harness{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.NewTerminalOfSize(&live, 80, 24),
		recorder: recordSession(testLog(t)),
	}

	call := agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}}

	self.currentTurn = Turn{isRunning: true, painter: self.newPainter(true)}
	self.events = append(self.events, call)
	self.currentTurn.painter.drawEvent(call)

	self.notifyStopped(notice)

	self.currentTurn.painter.close(dynamic.Cancelled)
	self.currentTurn = Turn{}
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
	self := &Harness{
		agent:    agent.New("", quietProvider{}, tools),
		screen:   output.New(&live),
		recorder: recordSession(testLog(t)),
	}
	livePainter := self.newPainter(true)

	events := []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "**Check** this"},
		{Kind: agent.ModelReasoningEvent, Text: "**Reading**\nLooking at the file. Need care."},
		{Kind: agent.ModelMessageEvent, Text: "The **first** answer.\n"},
		{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", Arguments: `{"path":"one.go"}`, FallbackRendering: agent.FallbackRendering{Subject: "old"}},
		{Kind: agent.ToolCallRequestEvent, ID: "2", Name: "gone", FallbackRendering: agent.FallbackRendering{Subject: "two.go"}},
		{Kind: agent.ToolCallResultEvent, ID: "1", Name: "read", Took: 2 * time.Second},
		{Kind: agent.ToolCallResultEvent, ID: "2", Name: "gone", Failed: true, Took: 3 * time.Second},
		{Kind: agent.ModelMessageEvent, Text: "Done."},
	}

	for _, event := range events {
		self.events = append(self.events, event)
		livePainter.drawEvent(event)
	}
	self.notifyStopped("Background processes killed (bash → sleep)")

	unansweredCall := agent.Event{Kind: agent.ToolCallRequestEvent, ID: "3", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "left.go"}}
	self.events = append(self.events, unansweredCall)
	livePainter.drawEvent(unansweredCall)
	livePainter.close(dynamic.Cancelled)
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
	callPainter := &Painter{screen: output.New(&screenOutput)}

	callPainter.drawEvent(agent.Event{Kind: agent.ModelReasoningEvent, Text: "checking"})
	callPainter.drawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}})
	callPainter.close(dynamic.Cancelled)

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

	current := truncate.Tool(buildSlowTool(
		slowToolBuilder("read").Focuses(func(tool.ToolCall) string { return "one.go" }),
	))
	testConversation := &Harness{
		agent:  agent.New("", quietProvider{}, []tool.Tool{current}),
		screen: output.New(&screenOutput),
	}

	testConversation.events = []agent.Event{{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "read",
		Arguments:         `{"path":"cmd/oh/one.go"}`,
		FallbackRendering: agent.FallbackRendering{Subject: "one.go:1-400"},
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

	testConversation := &Harness{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&screenOutput),
	}

	testConversation.events = []agent.Event{{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "divine",
		Arguments:         `{"path":"one.go"}`,
		FallbackRendering: agent.FallbackRendering{Subject: "one.go:1-400"},
	}}

	testConversation.replay()

	if !strings.Contains(screenOutput.String(), "one.go:1-400") {
		t.Errorf("expected what it looked like at the time, got %q", screenOutput.String())
	}
}

type quietProvider struct{}

func (quietProvider) Configure(string, []tool.Definition)   {}
func (quietProvider) AddUserMessage(string)                 {}
func (quietProvider) AddToolResults([]agent.ToolCallResult) {}
func (quietProvider) Dump() []json.RawMessage               { return nil }
func (quietProvider) Load([]json.RawMessage)                {}

func (quietProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	return agent.Reply{}, nil
}

func testConversation(t *testing.T, screenOutput *bytes.Buffer) *Harness {
	t.Helper()

	log, err := store.Create(t.TempDir(), store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = log.Close() })

	backend := quietProvider{}

	return &Harness{
		agent:              agent.New("", backend, nil),
		screen:             output.New(screenOutput),
		recorder:           recordSession(log),
		mode:               caps.NewMode(caps.Read | caps.Write),
		getOnWithItMessage: builtInConfig(t).GetOnWithItMessage,
	}
}

func completeTurn(self *Harness) {
	self.start("are you there")

	for report := range self.currentTurn.events {
		self.takeTurn(report)
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

func TestAStoppedTurnIsNotAnnouncedInTheScrollback(t *testing.T) {
	var screenOutput bytes.Buffer

	self := testConversation(t, &screenOutput)

	self.start("are you there")
	self.currentTurn.isCancelled = true
	self.currentTurn.cancel()

	for report := range self.currentTurn.events {
		self.takeTurn(report)
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
	callPainter := &Painter{screen: output.New(&screenOutput)}

	callPainter.drawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "bash", FallbackRendering: agent.FallbackRendering{Subject: "echo hello"}})
	callPainter.close(dynamic.Done)

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

	callPainter := &Painter{screen: output.New(&screenOutput)}

	callPainter.drawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}})
	callPainter.drawEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "never mind"})

	if callPainter.toolBlock != nil {
		t.Fatal("expected the block to be closed by the next thing asked")
	}

	callPainter.drawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "2", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "two.go"}})

	if got := callPainter.rows["2"]; got != 0 {
		t.Errorf("expected the call to open a block of its own, got row %d", got)
	}

	callPainter.close(dynamic.Cancelled)
}

func TestARedrawDuringATurnHandsTheOpenBlockToTheTurn(t *testing.T) {
	blocksBefore := blocksStillRunning(t)

	var screenOutput bytes.Buffer

	testConversation := &Harness{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&screenOutput),
	}

	testConversation.currentTurn = Turn{isRunning: true, painter: testConversation.newPainter(true)}

	testConversation.events = []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "read it"},
		{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}},
	}

	previousPainter := testConversation.currentTurn.painter

	testConversation.redraw()

	if testConversation.currentTurn.painter == previousPainter {
		t.Fatal("expected the turn to be given the painter that drew the replay")
	}

	if testConversation.currentTurn.painter.toolBlock == nil {
		t.Fatal("expected the unanswered call to be on a block that is open again")
	}

	testConversation.currentTurn.painter.drawEvent(agent.Event{Kind: agent.ToolCallResultEvent, ID: "1", Name: "read", Took: time.Second})

	if testConversation.currentTurn.painter.toolBlock != nil {
		t.Error("expected the block to close once every call had reported")
	}

	if got := blocksStillRunning(t); got > blocksBefore {
		t.Errorf("expected nothing left redrawing, got %d blocks where there were %d", got, blocksBefore)
	}
}

func TestARedrawDuringProvisionalReasoningRestoresTheOpenBlock(t *testing.T) {
	var screenOutput bytes.Buffer
	testConversation := &Harness{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&screenOutput),
		events: []agent.Event{{Kind: agent.UserMessageEvent, Text: "think"}},
	}
	testConversation.currentTurn = Turn{isRunning: true, painter: testConversation.newPainter(true)}
	testConversation.currentTurn.painter.drawDelta(agent.Delta{
		Kind: agent.ModelReasoningEvent,
		Text: "provisional thought",
	})

	testConversation.redraw()

	got := testConversation.currentTurn.painter.provisionalDelta()
	if got.Kind != agent.ModelReasoningEvent || got.Text != "provisional thought" {
		t.Errorf("got provisional delta %+v", got)
	}
}

func TestAStreamingMermaidDiagramKeepsItsLastValidRendering(t *testing.T) {
	const columns = 100
	var screenOutput bytes.Buffer
	painter := &Painter{screen: output.NewTerminalOfSize(&screenOutput, columns, 24), isRunning: true}

	painter.drawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	valid := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if !strings.Contains(valid, "►") || strings.Contains(valid, "graph LR") {
		t.Fatalf("expected the first valid prefix to render, got %q", valid)
	}

	painter.drawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	invalid := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if invalid != valid {
		t.Errorf("invalid prefix replaced the last valid diagram\nvalid:\n%s\ninvalid:\n%s", valid, invalid)
	}

	painter.drawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: " C"})
	nextValid := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if !strings.Contains(nextValid, "C") || strings.Contains(nextValid, "graph LR") {
		t.Errorf("expected the next valid prefix to replace the cached diagram, got %q", nextValid)
	}
}

func TestARedrawKeepsTheLastValidStreamingMermaidDiagram(t *testing.T) {
	const columns = 100
	var screenOutput bytes.Buffer
	testConversation := &Harness{screen: output.NewTerminalOfSize(&screenOutput, columns, 24)}
	testConversation.currentTurn = Turn{isRunning: true, painter: testConversation.newPainter(true)}

	testConversation.currentTurn.painter.drawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	valid := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	testConversation.currentTurn.painter.drawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	testConversation.redraw()

	redrawn := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if redrawn != valid {
		t.Errorf("redraw replaced the last valid diagram\nvalid:\n%s\nredrawn:\n%s", valid, redrawn)
	}
}

func TestACompletedInvalidMermaidDiagramFallsBackToSource(t *testing.T) {
	const columns = 100
	const invalid = "```mermaid\ngraph LR\nA --> B\nB -->\n```"
	var screenOutput bytes.Buffer
	painter := &Painter{screen: output.NewTerminalOfSize(&screenOutput, columns, 24), isRunning: true}

	painter.drawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	painter.drawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	painter.drawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: invalid})

	completed := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if !strings.Contains(completed, "graph LR") || !strings.Contains(completed, "B -->") {
		t.Errorf("expected completed invalid Mermaid to fall back to source, got %q", completed)
	}
}

func TestAnAnswerStreamedIsTheSameAsTheAnswerReplayed(t *testing.T) {
	const answer = "# Findings\n\nThe **first** thing is `read`.\n\n- one\n- two\n"

	var live bytes.Buffer

	livePainter := &Painter{screen: output.New(&live), isRunning: true}

	for _, delta := range deltas(answer, 10) {
		livePainter.drawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: delta})
	}
	livePainter.drawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: answer})
	livePainter.screen.End()

	var replayOutput bytes.Buffer

	replayPainter := &Painter{screen: output.New(&replayOutput)}
	replayPainter.drawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: answer})
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

func TestCallEmphasisSourceIsDerivedFromRecordedArguments(t *testing.T) {
	current := tool.Implement(
		tool.Definition{
			Name:        "bash",
			Description: "",
			Schema:      tool.Schema{tool.String("path", "command")},
		},
		func(args fakeArgs) (string, string) {
			subject, _, _ := strings.Cut(args.Path, "\n")
			return subject, ""
		},
	).SyntaxFrom("bash", func(args fakeArgs, _ string) string {
		return args.Path
	}).Plain(func(context.Context, fakeArgs) (string, error) { return "", nil })
	conversation := &Harness{agent: agent.New("", quietProvider{}, []tool.Tool{current})}

	fallback := conversation.newPainter(false).describe(agent.Event{
		Name:      "bash",
		Arguments: `{"path":"cat <<EOF\none\nEOF"}`,
	})

	if got, want := fallback.Emphasis.Source, "cat <<EOF\none\nEOF"; got != want {
		t.Errorf("got source %q, want %q", got, want)
	}
}

func TestWorkspacePrefixIsOmittedFromRenderedCallPaths(t *testing.T) {
	const workspaceDir = "/home/alice/project"

	t.Setenv("HOME", "/home/alice")

	current := tool.Implement(
		tool.Definition{
			Name:        "read",
			Description: "",
			Schema:      tool.Schema{tool.String("path", "file")},
		},
		func(args fakeArgs) (string, string) { return args.Path, pathutil.Shorten(args.Path) },
	).FocusPath().Plain(func(context.Context, fakeArgs) (string, error) { return "", nil })
	testConversation := &Harness{
		agent:        agent.New("", quietProvider{}, []tool.Tool{current}),
		workspaceDir: workspaceDir,
	}

	fallback := testConversation.newPainter(false).describe(agent.Event{
		Name:      "read",
		Arguments: `{"path":"/home/alice/project/cmd/oh/draw.go"}`,
	})

	wantEmphasis := tool.Emphasis{Kind: tool.EmphasisFocus, Value: "draw.go"}
	if fallback.Emphasis != wantEmphasis {
		t.Fatalf("unexpected emphasis %#v", fallback.Emphasis)
	}
	if fallback.Subject != "cmd/oh/draw.go" || fallback.Note != "cmd/oh/draw.go" {
		t.Errorf("got rendering %q and detail %q, want the workspace prefixes omitted", fallback.Subject, fallback.Note)
	}
}

func TestPathPrefixesAreShortened(t *testing.T) {
	const workspaceDir = "/home/alice/project"

	t.Setenv("HOME", "/home/alice")

	callPainter := &Painter{workspaceDir: workspaceDir}
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

	callPainter := &Painter{workspaceDir: "/home/alice/project"}
	fallback := callPainter.describe(agent.Event{
		Name: "removed",
		FallbackRendering: agent.FallbackRendering{
			Subject: "/home/alice/project/file.go",
			Note:    "/home/alice/reference/file.go",
		},
	})

	if fallback.Subject != "file.go" || fallback.Note != "~/reference/file.go" {
		t.Errorf("got subject %q and qualifier %q, want both path prefixes shortened", fallback.Subject, fallback.Note)
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

	testConversation := &Harness{
		agent: agent.New("", quietProvider{}, []tool.Tool{refusing}),
	}

	fallback := testConversation.newPainter(false).describe(agent.Event{
		Name:              "shout",
		Arguments:         `{"message":"oi"}`,
		FallbackRendering: agent.FallbackRendering{Subject: ""},
	})

	if fallback.Note != "" || fallback.Emphasis != (tool.Emphasis{}) {
		t.Fatalf("unexpected call description %q, %#v", fallback.Note, fallback.Emphasis)
	}
	if fallback.Subject != "oi" {
		t.Errorf("got %q, want the arguments described again from the record", fallback.Subject)
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

func TestAnAsideStandsBetweenTheCallsItArrivedAmong(t *testing.T) {
	var screenOutput bytes.Buffer
	painter := &Painter{screen: output.NewTerminalOfSize(&screenOutput, 80, 24), isRunning: true}

	painter.drawEvent(agent.Event{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "read",
		FallbackRendering: agent.FallbackRendering{Subject: "one.txt"},
	})
	painter.drawEvent(agent.Event{Kind: agent.HarnessMessageEvent, Text: "something happened"})

	if painter.toolBlock == nil {
		t.Fatal("expected the block to stay open under the aside")
	}

	painter.drawEvent(agent.Event{Kind: agent.ToolCallResultEvent, ID: "1", Took: time.Second})

	rows := visibleScreen(t, screenOutput.String(), 80)
	rows = slices.DeleteFunc(rows, func(row string) bool { return strings.TrimSpace(row) == "" })

	if len(rows) != 2 {
		t.Fatalf("expected the call and the aside on rows of their own, got %q", rows)
	}
	if !strings.Contains(rows[0], "one.txt") || !strings.Contains(rows[0], "✓") {
		t.Errorf("expected the call to keep its result above the aside, got %q", rows[0])
	}
	if !strings.Contains(rows[1], "something happened") {
		t.Errorf("expected the aside under the call, got %q", rows[1])
	}
}

func TestCompletedReasoningBlocksRemainSeparateInTheJournal(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}

	var screenOutput bytes.Buffer
	self := &Harness{
		screen:   output.New(&screenOutput),
		recorder: recordSession(log),
	}
	self.currentTurn = Turn{isRunning: true, painter: self.newPainter(true)}

	userMessage := agent.Event{Kind: agent.UserMessageEvent, Text: "think twice"}
	self.takeTurn(TurnEvent{update: agent.Update{Event: &userMessage}})
	firstDelta := agent.Delta{Kind: agent.ModelReasoningEvent, Text: "**First "}
	self.takeTurn(TurnEvent{update: agent.Update{Delta: &firstDelta}})
	firstBlock := agent.Event{Kind: agent.ModelReasoningEvent, Text: "**First block**"}
	self.takeTurn(TurnEvent{update: agent.Update{Event: &firstBlock}})
	secondDelta := agent.Delta{Kind: agent.ModelReasoningEvent, Text: "**Second block**"}
	self.takeTurn(TurnEvent{update: agent.Update{Delta: &secondDelta}})
	secondBlock := agent.Event{Kind: agent.ModelReasoningEvent, Text: "**Second block**"}
	self.takeTurn(TurnEvent{update: agent.Update{Event: &secondBlock}})

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}

	var reasoning []string
	for _, event := range storedSession.Events {
		if event.Kind == agent.ModelReasoningEvent {
			reasoning = append(reasoning, event.Text)
		}
	}
	want := []string{"**First block**", "**Second block**"}
	if !slices.Equal(reasoning, want) {
		t.Errorf("got reasoning blocks %q, want %q", reasoning, want)
	}

	plain := style.Plain(screenOutput.String())
	if !strings.Contains(plain, "First block\nSecond block") {
		t.Errorf("expected separate reasoning lines, got %q", plain)
	}
}

func TestIncompleteReasoningIsErasedBeforeAFailure(t *testing.T) {
	var screenOutput bytes.Buffer
	painter := &Painter{screen: output.New(&screenOutput), isRunning: true}

	painter.drawDelta(agent.Delta{Kind: agent.ModelReasoningEvent, Text: "half a thought"})
	painter.drawEvent(agent.Event{Kind: agent.FailureEvent, Text: "stream failed"})
	painter.screen.End()

	plain := style.Plain(screenOutput.String())
	if strings.Contains(plain, "half a thought") {
		t.Errorf("incomplete reasoning reached scrollback: %q", plain)
	}
	if !strings.Contains(plain, "stream failed") {
		t.Errorf("expected the failure, got %q", plain)
	}
}

func TestDiscardedReasoningLeavesTheSameScreenAsReplay(t *testing.T) {
	var liveOutput bytes.Buffer
	self := &Harness{
		screen:   output.NewTerminalOfSize(&liveOutput, 80, 24),
		recorder: recordSession(testLog(t)),
	}
	self.currentTurn = Turn{isRunning: true, painter: self.newPainter(true)}

	completeThought := agent.Event{Kind: agent.ModelReasoningEvent, Text: "complete thought"}
	self.takeTurn(TurnEvent{update: agent.Update{Event: &completeThought}})
	incompleteThought := agent.Delta{Kind: agent.ModelReasoningEvent, Text: "incomplete thought"}
	self.takeTurn(TurnEvent{update: agent.Update{Delta: &incompleteThought}})
	failure := agent.Event{Kind: agent.FailureEvent, Text: "stream failed"}
	self.takeTurn(TurnEvent{update: agent.Update{Event: &failure}})
	self.screen.End()

	var replayOutput bytes.Buffer
	replayPainter := &Painter{screen: output.NewTerminalOfSize(&replayOutput, 80, 24)}
	replayPainter.drawEvent(completeThought)
	replayPainter.drawEvent(failure)
	replayPainter.screen.End()

	live := visibleScreen(t, liveOutput.String(), 80)
	replayed := visibleScreen(t, replayOutput.String(), 80)
	if !slices.Equal(live, replayed) {
		t.Errorf("discarded reasoning changed the settled screen\nlive:\n%s\nreplayed:\n%s", strings.Join(live, "\n"), strings.Join(replayed, "\n"))
	}
}
