package painter

import (
	"bytes"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/output"
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
