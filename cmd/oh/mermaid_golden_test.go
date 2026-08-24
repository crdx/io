package main

import (
	"bytes"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/output"
)

func TestMermaidStreamingDrawsWhatItDrewBefore(t *testing.T) {
	compareWithGolden(t, "mermaid-streaming", ".screen", map[string]func() string{
		"1 first valid prefix": func() string {
			return mermaidStreamingScreen(t, "```mermaid\ngraph LR\nA --> B")
		},
		"2 invalid extension": func() string {
			return mermaidStreamingScreen(t, "```mermaid\ngraph LR\nA --> B", "\nB -->")
		},
		"3 invalid after redraw": func() string {
			return mermaidStreamingRedrawnScreen(t)
		},
		"4 next valid prefix": func() string {
			return mermaidStreamingScreen(t, "```mermaid\ngraph LR\nA --> B", "\nB -->", " C")
		},
		"5 completed invalid diagram": func() string {
			return completedInvalidMermaidScreen(t)
		},
	})
}

func mermaidStreamingScreen(t *testing.T, deltas ...string) string {
	t.Helper()
	var screenOutput bytes.Buffer
	painter := &Painter{Screen: output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines), IsRunning: true}
	for _, delta := range deltas {
		painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: delta})
	}
	return shown(t, screenOutput.String(), replayColumns)
}

func mermaidStreamingRedrawnScreen(t *testing.T) string {
	t.Helper()
	var screenOutput bytes.Buffer
	testConversation := &Harness{screen: output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)}
	testConversation.currentTurn = Turn{Stream: testRunningTurnStream(), painter: testConversation.newPainter(true)}
	testConversation.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	testConversation.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	testConversation.redraw()
	return shown(t, screenOutput.String(), replayColumns)
}

func completedInvalidMermaidScreen(t *testing.T) string {
	t.Helper()
	const invalid = "```mermaid\ngraph LR\nA --> B\nB -->\n```"
	var screenOutput bytes.Buffer
	painter := &Painter{Screen: output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines), IsRunning: true}
	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	painter.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: invalid})
	return shown(t, screenOutput.String(), replayColumns)
}
