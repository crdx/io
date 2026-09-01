package painter

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/pathgrant"
	"crdx.org/io/cmd/oh/startup"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
)

func TestStartupDrawingUsesTheScreensTextSizingSupport(t *testing.T) {
	for name, isSupported := range map[string]bool{"supported": true, "unsupported": false} {
		t.Run(name, func(t *testing.T) {
			var screenOutput bytes.Buffer
			screen := output.NewTerminalOfSize(&screenOutput, 80, 24)
			screen.SetTextSizingSupported(isSupported)
			paint := New(screen, false, nil, nil, output.StreamingModeLine)
			paint.DrawEvent(startup.NewEvent(time.Millisecond, startup.Info{Session: "brave-otter"}))

			got := strings.Contains(screenOutput.String(), "\x1b]66;")
			if got != isSupported {
				t.Errorf("sized startup presence = %t, want %t", got, isSupported)
			}
		})
	}
}

func TestPathGrantEventsAreDrawnFromTheirStructuredState(t *testing.T) {
	var screenOutput bytes.Buffer
	paint := New(output.NewTerminalOfSize(&screenOutput, 80, 24), false, nil, nil, output.StreamingModeLine)
	event, err := pathgrant.ChangeEvent("/reference", []pathgrant.Grant{{
		Path:   "/reference",
		Access: pathgrant.ReadAccess,
	}})
	if err != nil {
		t.Fatal(err)
	}

	paint.DrawEvent(event)
	if drawn := style.Plain(screenOutput.String()); !strings.Contains(drawn, "Granted temporary read-only access to /reference.") {
		t.Errorf("got drawing %q", drawn)
	}
}

func TestOnlyTerminalConversationMessagesContainHyperlinks(t *testing.T) {
	for eventName, kind := range map[string]agent.Kind{
		"assistant": agent.ModelMessageEvent,
		"user":      agent.UserMessageEvent,
	} {
		for streamName, isTerminal := range map[string]bool{"terminal": true, "stream": false} {
			t.Run(eventName+"/"+streamName, func(t *testing.T) {
				var screenOutput bytes.Buffer
				screen := output.New(&screenOutput)
				if isTerminal {
					screen = output.NewTerminalOfSize(&screenOutput, 80, 24)
				}
				paint := New(screen, false, nil, "", output.StreamingModeLine)
				paint.DrawEvent(agent.Event{Kind: kind, Text: "[docs](https://example.test)"})

				hasHyperlink := strings.Contains(screenOutput.String(), "\x1b]8;;https://example.test\x1b\\")
				if hasHyperlink != isTerminal {
					t.Errorf("hyperlink presence = %t in %q", hasHyperlink, screenOutput.String())
				}
			})
		}
	}
}

func TestNoUnfinishedLineEverWithdrawsARowThatWasDrawn(t *testing.T) {
	answers := map[string]string{
		"a bullet marker":       "Here is the list.\n\n- one\n- two\n- three\n\nAnd after it.\n",
		"a nested marker":       "Here is the list.\n\n- one\n  - deeper\n  - deeper still\n- two\n\nAnd after it.\n",
		"an ordered marker":     "Here is the list.\n\n1. one\n2. two\n3. three\n\nAnd after it.\n",
		"a task marker":         "Here is the list.\n\n- [ ] one\n- [x] two\n\nAnd after it.\n",
		"a quoted marker":       "Here is the quote.\n\n> - one\n> - two\n\nAnd after it.\n",
		"a heading marker":      "Here is the heading.\n\n## One\n\nAnd after it.\n",
		"an indented code line": "Here is the code.\n\n    one = 1\n    two = 2\n\nAnd after it.\n",
		"a table row":           "Here is the table.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\nAnd after it.\n",
	}

	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			for _, deltaRunes := range []int{1, 2, 3, 4, 5, 7} {
				t.Run(strconv.Itoa(deltaRunes), func(t *testing.T) {
					streamWithoutWithdrawing(t, answer, deltaRunes)
				})
			}
		})
	}
}

func streamWithoutWithdrawing(t *testing.T, answer string, deltaRunes int) {
	t.Helper()

	const columns = 40

	var screenOutput bytes.Buffer

	paint := New(output.NewTerminalOfSize(&screenOutput, columns, 24), false, nil, nil, output.StreamingModeLine)

	runes := []rune(answer)
	drawnRowCount := 0

	var arrived strings.Builder

	for at := 0; at < len(runes); at += deltaRunes {
		delta := string(runes[at:min(at+deltaRunes, len(runes))])
		arrived.WriteString(delta)

		screenOutput.Reset()
		paint.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: delta})

		rendered := len(markdown.Render(arrived.String(), columns))

		if paint.answer.drawnRowCount < drawnRowCount && rendered >= drawnRowCount {
			t.Errorf(
				"delta %q took the drawn rows from %d down to %d while %d were rendered",
				delta, drawnRowCount, paint.answer.drawnRowCount, rendered,
			)
		}
		drawnRowCount = paint.answer.drawnRowCount

		if drawsNothingButErases(screenOutput.String()) {
			t.Errorf("delta %q sent a frame that erased without drawing: %q", delta, screenOutput.String())
		}
	}
}

func drawsNothingButErases(frame string) bool {
	payload := style.Plain(frame)
	for _, wrapper := range []string{"\x1b[?2026h", "\x1b[?2026l", "\x1b[?25l", "\x1b[?25h", "\x1b[?7l"} {
		payload = strings.ReplaceAll(payload, wrapper, "")
	}

	if payload == "" || !strings.Contains(frame, "\x1b[K") && !strings.Contains(frame, "\x1b[J") {
		return false
	}

	return strings.TrimLeft(cursorMotion.ReplaceAllString(payload, ""), "\r") == ""
}

var cursorMotion = regexp.MustCompile(`\x1b\[[0-9]*[ABCDJK]`)

func TestNewQuestionClosesOldToolBlockAndResetsRows(t *testing.T) {
	paint := New(output.New(&bytes.Buffer{}), false, nil, nil, output.StreamingModeLine)
	paint.DrawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}})
	paint.DrawEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "never mind"})
	if paint.toolBlock != nil {
		t.Fatal("old tool block remained open")
	}

	paint.DrawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "2", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "two.go"}})
	if row := paint.rows["2"]; row != 0 {
		t.Errorf("new block started at row %d", row)
	}
}

func TestHarnessAsideKeepsToolBlockOpen(t *testing.T) {
	paint := New(output.New(&bytes.Buffer{}), true, nil, nil, output.StreamingModeLine)
	paint.DrawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read"})
	paint.DrawEvent(agent.Event{Kind: agent.HarnessMessageEvent, Text: "something happened"})
	if paint.toolBlock == nil {
		t.Fatal("aside closed the tool block")
	}
}

func TestARetryIsDrawnFromWhatItWasRatherThanFromWhatItSaid(t *testing.T) {
	var screenOutput bytes.Buffer

	paint := New(output.New(&screenOutput), false, nil, nil, output.StreamingModeLine)
	paint.DrawEvent(agent.Event{
		Kind:    agent.RetryingEvent,
		Text:    "The stream ended before the response did\nand a second line nobody needs",
		Attempt: 2,
		Took:    500 * time.Millisecond,
	})

	drawn := style.Plain(screenOutput.String())

	for _, want := range []string{
		"[#2] Request failed",
		"retrying in 0.5s",
		"The stream ended before the response did",
	} {
		if !strings.Contains(drawn, want) {
			t.Errorf("expected %q to be drawn, got %q", want, drawn)
		}
	}

	if strings.Contains(drawn, "nobody needs") {
		t.Errorf("expected only the first line of what stopped it, got %q", drawn)
	}
}

func TestARetryShowsTheCallThatProvokedIt(t *testing.T) {
	var screenOutput bytes.Buffer

	paint := New(output.New(&screenOutput), false, nil, nil, output.StreamingModeLine)
	paint.DrawEvent(agent.Event{
		Kind:      agent.RetryingEvent,
		Text:      "The read tool call did not contain a JSON object",
		Name:      "read",
		Arguments: `{"path": "one.go",, "limit": 20}`,
		Attempt:   1,
	})

	drawn := style.Plain(screenOutput.String())

	if !strings.Contains(drawn, "[#1] Request failed; retrying") {
		t.Errorf("expected the first retry to show its attempt number, got %q", drawn)
	}
	if !strings.Contains(drawn, `{"path": "one.go",, "limit": 20}`) {
		t.Errorf("expected the call that provoked the retry to be drawn, got %q", drawn)
	}
}

func TestALongFaultedCallIsCutRatherThanDrawnWhole(t *testing.T) {
	var screenOutput bytes.Buffer

	arguments := `{"text": "` + strings.Repeat("x", 4*retryArgumentsCells) + `"}`

	paint := New(output.New(&screenOutput), false, nil, nil, output.StreamingModeLine)
	paint.DrawEvent(agent.Event{
		Kind:      agent.RetryingEvent,
		Text:      "The write tool call did not contain a JSON object",
		Name:      "write",
		Arguments: arguments,
		Attempt:   1,
	})

	drawn := style.Plain(screenOutput.String())

	if strings.Contains(drawn, arguments) {
		t.Error("expected a long call to be cut rather than drawn whole")
	}
	if !strings.Contains(drawn, width.Ellipsis) {
		t.Errorf("expected the cut to be marked, got %q", drawn)
	}
	if !strings.Contains(drawn, `{"text": "xxx`) {
		t.Errorf("expected what was kept to start the call, got %q", drawn)
	}
}
