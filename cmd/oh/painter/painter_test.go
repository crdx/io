package painter

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/style"
)

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
		"Attempt 2 failed",
		"asking again in 0.5s",
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
