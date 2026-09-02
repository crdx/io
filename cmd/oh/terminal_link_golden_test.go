package main

import (
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/painter"
)

func TestSpecialLinksDrawWhatTheyDrewBefore(t *testing.T) {
	passes := map[string]func() string{
		"email autolink": func() string {
			return drawEmailLink(t)
		},
		"pending message": func() string {
			return drawPendingLink(t)
		},
		"pending message with a heading and a list": func() string {
			return drawPendingMarkdown(t)
		},
	}
	compareWithGolden(t, "special-links", ".ansi", passes)

	screenPasses := map[string]func() string{}
	for name, pass := range passes {
		screenPasses[name] = func() string {
			return shown(t, pass(), terminalInputColumns)
		}
	}
	compareWithGolden(t, "special-links", ".screen", screenPasses)
}

func drawEmailLink(t *testing.T) string {
	t.Helper()

	address := strings.Join([]string{"person", "example.test"}, "@")
	var screenOutput strings.Builder
	paint := painter.New(
		output.NewTerminalOfSize(&screenOutput, terminalInputColumns, replayLines),
		false,
		nil,
		nil,
		output.StreamingModeLine,
	)
	paint.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: address})

	return strings.ReplaceAll(screenOutput.String(), address, "person-at-example.test")
}

func drawPendingLink(t *testing.T) string {
	t.Helper()

	var screenOutput strings.Builder
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.NewTerminalOfSize(&screenOutput, terminalInputColumns, replayLines)
	self.pending.add(agent.Event{
		Kind: agent.UserMessageEvent,
		Text: "read [the pending reference](https://example.test/pending)",
	}, agent.Event{})
	self.refreshPendingMessages()

	return screenOutput.String()
}

func drawPendingMarkdown(t *testing.T) string {
	t.Helper()

	var screenOutput strings.Builder
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.NewTerminalOfSize(&screenOutput, terminalInputColumns, replayLines)
	self.pending.add(agent.Event{
		Kind: agent.UserMessageEvent,
		Text: "# Heading\n\n- first item\n- second item",
	}, agent.Event{})
	self.refreshPendingMessages()

	return screenOutput.String()
}
