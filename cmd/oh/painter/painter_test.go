package painter

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/output"
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
			paint := New(screen, false, nil, "")
			paint.DrawEvent(startup.NewEvent(time.Millisecond, startup.Info{Session: "brave-otter"}))

			got := strings.Contains(screenOutput.String(), "\x1b]66;")
			if got != isSupported {
				t.Errorf("sized startup presence = %t, want %t", got, isSupported)
			}
		})
	}
}

func TestNewQuestionClosesOldToolBlockAndResetsRows(t *testing.T) {
	paint := New(output.New(&bytes.Buffer{}), false, nil, "")
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
	paint := New(output.New(&bytes.Buffer{}), true, nil, "")
	paint.DrawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read"})
	paint.DrawEvent(agent.Event{Kind: agent.HarnessMessageEvent, Text: "something happened"})
	if paint.toolBlock == nil {
		t.Fatal("aside closed the tool block")
	}
}

func TestARetryIsDrawnFromWhatItWasRatherThanFromWhatItSaid(t *testing.T) {
	var screenOutput bytes.Buffer

	paint := New(output.New(&screenOutput), false, nil, "")
	paint.DrawEvent(agent.Event{
		Kind:    agent.RetryingEvent,
		Text:    "The stream ended before the response did\nand a second line nobody needs",
		Attempt: 2,
		Took:    500 * time.Millisecond,
	})

	drawn := style.Plain(screenOutput.String())

	for _, want := range []string{
		"Request failed on attempt 2",
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

	paint := New(output.New(&screenOutput), false, nil, "")
	paint.DrawEvent(agent.Event{
		Kind:      agent.RetryingEvent,
		Text:      "The read tool call did not contain a JSON object",
		Name:      "read",
		Arguments: `{"path": "one.go",, "limit": 20}`,
		Attempt:   1,
	})

	drawn := style.Plain(screenOutput.String())

	if !strings.Contains(drawn, "Request failed; retrying") {
		t.Errorf("expected the first retry to omit its attempt number, got %q", drawn)
	}
	if !strings.Contains(drawn, `{"path": "one.go",, "limit": 20}`) {
		t.Errorf("expected the call that provoked the retry to be drawn, got %q", drawn)
	}
}

func TestALongFaultedCallIsCutRatherThanDrawnWhole(t *testing.T) {
	var screenOutput bytes.Buffer

	arguments := `{"text": "` + strings.Repeat("x", 4*retryArgumentsCells) + `"}`

	paint := New(output.New(&screenOutput), false, nil, "")
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
