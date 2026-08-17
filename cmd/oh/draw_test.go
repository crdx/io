package main

import (
	"bytes"
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/session"
	"crdx.org/io/cmd/oh/status"
	"crdx.org/io/cmd/oh/theme"
	"crdx.org/io/tool"
	"crdx.org/io/tool/middleware/truncate"
)

// What was typed is shown in the input, which is drawn over on the way past and is no part of the
// conversation. Its markdown rendering is said again as a line of its own to put it in scrollback.
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

// A turn cancelled partway is stored with its calls announced and some of them never answered. The
// block those calls opened is redrawn on a ticker of its own until it is closed, so a replay that
// walks away from one leaves it drawing over the conversation that follows.
func TestReplayingACallThatWasNeverAnsweredLeavesNothingRunning(t *testing.T) {
	goroutinesBefore := runtime.NumGoroutine()

	var screenOutput bytes.Buffer

	testConversation := &conversation{
		assistant: agent.New("", quietProvider{}, nil),
		screen:    output.New(&screenOutput),
	}

	testConversation.restore(&session.Session{
		Events: []agent.Event{
			{Kind: agent.Prompt, Text: "read them both"},
			{Kind: agent.Call, ID: "1", Name: "read", Render: "one.go"},
			{Kind: agent.Call, ID: "2", Name: "read", Render: "two.go"},
			{Kind: agent.Result, ID: "1", Name: "read", Text: "package one"},
		},
	})

	if got := runtime.NumGoroutine(); got > goroutinesBefore {
		t.Errorf("expected nothing left running, got %d goroutines where there were %d", got, goroutinesBefore)
	}
}

// A redraw has nothing on the screen to work from, so it says the conversation again from what it
// kept. What the harness said itself is part of that: it was on the screen, so it comes back.
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

// T30: every durable kind of output takes the same bytes to the screen while it happens and when
// the transcript is replayed from nothing.
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

	if live.String() != replayOutput.String() {
		t.Errorf("live conversation %q differs from replayed conversation %q", live.String(), replayOutput.String())
	}
}

// A summary arrives as a bold heading with a sentence or two under it, and the harness gives a
// thought one line, so what was several lines becomes one.
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

// How a call is shown is the tool's business and the tool's alone, so what a stored conversation
// was shown as when it ran is redrawn the way the tool draws it today.
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

// A tool the conversation outlived cannot say what its call was, so what the call looked like at
// the time is all there is left to show.
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

	log, err := session.Create(t.TempDir(), session.Header{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = log.Close() })

	backend := quietProvider{}

	return &conversation{
		assistant: agent.New("", backend, nil),
		screen:    output.New(screenOutput),
		log:       log,
		mode:      NewMode(capRead | capWrite),
	}
}

func completeTurn(self *conversation) {
	self.start("are you there")

	for report := range self.turn.events {
		self.take(report)
	}

	self.finish()
}

// The turn's context is cancelled on the way out of every turn, the ones that ran to the end
// included, so what the context says is not what the person at the keyboard asked for.
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

// A cancelled turn leaves calls no result ever comes for, and the block holding them is closed when
// the turn ends. Replaying it has no turn to end, so the next thing asked is what closes it: a
// block left open would take the calls of every turn after it, and never close at all.
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
}

// A redraw during a turn opens the block its unanswered calls sit on again, and the turn is given
// the painter that drew it, so a result still in flight lands on the row it was already on rather
// than opening a block of its own under the conversation.
func TestARedrawDuringATurnHandsTheOpenBlockToTheTurn(t *testing.T) {
	goroutinesBefore := runtime.NumGoroutine()

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

	if got := runtime.NumGoroutine(); got > goroutinesBefore {
		t.Errorf("expected nothing left running, got %d goroutines where there were %d", got, goroutinesBefore)
	}
}

// The live path and the replay path are one function called with different arguments: an answer
// arriving in pieces and the same answer replayed from the transcript are the same rows.
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
