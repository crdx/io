package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"runtime"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/status"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/theme"
	"crdx.org/io/tool"
	"crdx.org/io/tool/middleware/truncate"
)

func TestWhatWasAskedIsRenderedIntoTheConversation(t *testing.T) {
	for _, live := range []bool{true, false} {
		var screenOutput bytes.Buffer

		painter := &painter{screen: output.New(&screenOutput), live: live}
		painter.draw(agent.Event{Kind: agent.Prompt, Text: "**weather**\n\n- today"})

		plain := theme.Plain(screenOutput.String())
		if !strings.Contains(plain, " weather\n \n • today") {
			t.Errorf("live=%v: expected the question's markdown to be rendered, got %q", live, plain)
		}

		if strings.Contains(plain, "> ") || strings.Contains(plain, "**") {
			t.Errorf("live=%v: expected no literal prompt or markdown markers, got %q", live, plain)
		}
	}
}

func TestASubmittedMessageHasBackgroundRowsAboveAndBelowIt(t *testing.T) {
	got := theme.Plain(renderSubmittedMessage("hello", 8))
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
			{Kind: agent.Call, ID: "1", Name: "read", Render: "one.go"},
			{Kind: agent.Call, ID: "2", Name: "read", Render: "two.go"},
			{Kind: agent.Result, ID: "1", Name: "read", Text: "package one"},
		},
	})

	if got := blocksStillRunning(t); got > blocksBefore {
		t.Errorf("expected the block to have been closed, got %d redrawing where there were %d", got, blocksBefore)
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

	testConversation.transcript = []entry{
		{event: agent.Event{Kind: agent.Prompt, Text: "what is the weather"}},
		{event: agent.Event{Kind: agent.Text, Text: "it is raining"}},
		{notice: "cancelled"},
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
			want: "load " + skillPath, painted: theme.Skill("golang"),
		},
		"a read of a file": {
			tool: "read", path: "cmd/oh/draw.go",
			want: "read cmd/oh/draw.go", painted: theme.Args("draw.go"),
		},
		"another tool": {tool: "grep", path: skillPath, want: "grep " + skillPath},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var screenOutput bytes.Buffer
			callPainter := &painter{screen: output.New(&screenOutput)}

			callPainter.draw(agent.Event{
				Kind: agent.Call, ID: "1", Name: test.tool, Render: test.path, Focus: path.Base(test.path),
			})
			callPainter.close(status.Done)

			if plain := theme.Plain(screenOutput.String()); !strings.Contains(plain, test.want) {
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
		Render: "/skills/golang/SKILL.md", Focus: "SKILL.md",
	})
	callPainter.close(status.Done)

	if strings.Contains(screenOutput.String(), theme.Args("SKILL.md")) {
		t.Errorf("got %q, want the file left dim", screenOutput.String())
	}
}

func TestAHarnessNoticeIsDrawnTheSameLiveAndReplayed(t *testing.T) {
	const notice = "Background processes killed (tmux: server → bash → sleep)"

	var live bytes.Buffer
	self := &conversation{
		assistant: agent.New("", quietProvider{}, nil),
		screen:    output.New(&live),
	}
	self.notify(theme.Stopped(notice))
	self.screen.End()

	var replayOutput bytes.Buffer
	self.screen = output.New(&replayOutput)
	self.replay()

	if live.String() != replayOutput.String() {
		t.Errorf("live notice %q differs from replayed notice %q", live.String(), replayOutput.String())
	}
}

func TestTheWholeConversationIsDrawnTheSameLiveAndReplayed(t *testing.T) {
	tools := []tool.Tool{slowTool("read")}

	var live bytes.Buffer
	self := &conversation{
		assistant: agent.New("", quietProvider{}, tools),
		screen:    output.New(&live),
	}
	livePainter := self.newPicasso(true)

	events := []agent.Event{
		{Kind: agent.Prompt, Text: "**Check** this"},
		{Kind: agent.Reasoning, Text: "**Reading**\nLooking at the file."},
		{Kind: agent.Text, Text: "The **first** "},
		{Kind: agent.Text, Text: "answer.\n"},
		{Kind: agent.Call, ID: "1", Name: "read", Arguments: `{"path":"one.go"}`, Render: "old"},
		{Kind: agent.Call, ID: "2", Name: "gone", Render: "two.go"},
		{Kind: agent.Result, ID: "1", Name: "read", Took: 2 * time.Second},
		{Kind: agent.Result, ID: "2", Name: "gone", Failed: true, Took: 3 * time.Second},
		{Kind: agent.Text, Text: "Done."},
	}

	for _, event := range events {
		self.record(event)
		livePainter.draw(event)
	}
	self.notify(theme.Stopped("Background processes killed (bash → sleep)"))

	unansweredCall := agent.Event{Kind: agent.Call, ID: "3", Name: "read", Render: "left.go"}
	self.record(unansweredCall)
	livePainter.draw(unansweredCall)
	livePainter.close(status.Cancelled)
	self.screen.End()

	var replayOutput bytes.Buffer
	self.screen = output.New(&replayOutput)
	self.replay()

	plain := theme.Plain(live.String())

	for _, want := range []string{"Check", "Looking at the file.", "first answer.", "one.go", "Done.", "Background processes killed"} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected %q on the screen, got %q", want, plain)
		}
	}

	if live.String() != replayOutput.String() {
		t.Errorf("live conversation %q differs from replayed conversation %q", live.String(), replayOutput.String())
	}
}

func TestAThoughtIsFlattenedOntoOneLine(t *testing.T) {
	tests := map[string]string{
		"Checking the sky.": "Checking the sky.",

		"**Fixing the cancel**\nThe call is never answered.": "Fixing the cancel · The call is never answered.",

		"**Reading**\n\n  Indented, and blank lines between.  \n": "Reading · Indented, and blank lines between.",

		"": "",
	}

	for thought, want := range tests {
		if got := flatten(thought); got != want {
			t.Errorf("flatten(%q) = %q, want %q", thought, got, want)
		}
	}
}

func TestAStoredCallIsShownTheWayItsToolShowsItNow(t *testing.T) {
	var screenOutput bytes.Buffer

	current := truncate.Tool(tool.Focus(slowTool("read"), func(tool.Call) string { return "one.go" }))
	testConversation := &conversation{
		assistant: agent.New("", quietProvider{}, []tool.Tool{current}),
		screen:    output.New(&screenOutput),
	}

	testConversation.transcript = []entry{{event: agent.Event{
		Kind:      agent.Call,
		ID:        "1",
		Name:      "read",
		Arguments: `{"path":"cmd/oh/one.go"}`,
		Render:    "one.go:1-400",
	}}}

	testConversation.replay()

	if strings.Contains(screenOutput.String(), "one.go:1-400") {
		t.Errorf("expected the stored rendering to be redrawn, got %q", screenOutput.String())
	}

	want := theme.Detail("cmd/oh/") + theme.Args("one.go")
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

	testConversation.transcript = []entry{{event: agent.Event{
		Kind:      agent.Call,
		ID:        "1",
		Name:      "divine",
		Arguments: `{"path":"one.go"}`,
		Render:    "one.go:1-400",
	}}}

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

func TestATurnThatWasStoppedSaysSo(t *testing.T) {
	var screenOutput bytes.Buffer

	self := testConversation(t, &screenOutput)

	self.start("are you there")
	self.turn.cancelled = true
	self.turn.stop()

	for report := range self.turn.events {
		self.take(report)
	}

	self.finish()

	plain := theme.Plain(screenOutput.String())
	if !strings.Contains(plain, "\n\nInterrupted\n") {
		t.Errorf("expected the interruption to have a line around it, got %q", plain)
	}

	if strings.Contains(screenOutput.String(), "context canceled") { // the stop is why it failed
		t.Errorf("expected the stop to be reported as a stop, got %q", screenOutput.String())
	}
}

func TestAShellCallIsDrawnAsAShellPrompt(t *testing.T) {
	var screenOutput bytes.Buffer
	callPainter := &painter{screen: output.New(&screenOutput), shell: "bash"}

	callPainter.draw(agent.Event{Kind: agent.Call, ID: "1", Name: "bash", Render: "echo hello"})
	callPainter.close(status.Done)

	plain := theme.Plain(screenOutput.String())
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

	callPainter.draw(agent.Event{Kind: agent.Call, ID: "1", Name: "read", Render: "one.go"})
	callPainter.draw(agent.Event{Kind: agent.Prompt, Text: "never mind"})

	if callPainter.block != nil {
		t.Fatal("expected the block to be closed by the next thing asked")
	}

	callPainter.draw(agent.Event{Kind: agent.Call, ID: "2", Name: "read", Render: "two.go"})

	if got := callPainter.rows["2"]; got != 0 {
		t.Errorf("expected the call to open a block of its own, got row %d", got)
	}

	callPainter.close(status.Cancelled) // the block it opened is this test's to shut
}

func TestARedrawDuringATurnHandsTheOpenBlockToTheTurn(t *testing.T) {
	blocksBefore := blocksStillRunning(t)

	var screenOutput bytes.Buffer

	testConversation := &conversation{
		assistant: agent.New("", quietProvider{}, nil),
		screen:    output.New(&screenOutput),
	}

	testConversation.turn = turn{running: true, painter: testConversation.newPicasso(true)}

	testConversation.transcript = []entry{
		{event: agent.Event{Kind: agent.Prompt, Text: "read it"}},
		{event: agent.Event{Kind: agent.Call, ID: "1", Name: "read", Render: "one.go"}},
	}

	previousPainter := testConversation.turn.painter

	testConversation.redraw()

	if testConversation.turn.painter == previousPainter {
		t.Fatal("expected the turn to be given the painter that drew the replay")
	}

	if testConversation.turn.painter.block == nil {
		t.Fatal("expected the unanswered call to be on a block that is open again")
	}

	testConversation.turn.painter.draw(agent.Event{Kind: agent.Result, ID: "1", Name: "read", Took: time.Second})

	if testConversation.turn.painter.block != nil {
		t.Error("expected the block to close once every call had reported")
	}

	if got := blocksStillRunning(t); got > blocksBefore {
		t.Errorf("expected nothing left redrawing, got %d blocks where there were %d", got, blocksBefore)
	}
}

func TestAnAnswerStreamedIsTheSameAsTheAnswerReplayed(t *testing.T) {
	const answer = "# Findings\n\nThe **first** thing is `read`.\n\n- one\n- two\n"

	var live bytes.Buffer

	livePainter := &painter{screen: output.New(&live), live: true}

	for _, delta := range deltas(answer, 10) {
		livePainter.draw(agent.Event{Kind: agent.Text, Text: delta})
	}

	livePainter.screen.End()

	var replayOutput bytes.Buffer

	replayPainter := &painter{screen: output.New(&replayOutput)}
	replayPainter.draw(agent.Event{Kind: agent.Text, Text: answer})
	replayPainter.screen.End()

	plain := theme.Plain(live.String())

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
