package main

import (
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/painter"
	"crdx.org/io/cmd/oh/pathgrant"
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
	grantEvent, err := pathgrant.ChangeEvent("/reference", []pathgrant.Grant{{Path: "/reference", Access: pathgrant.ReadAccess}})
	if err != nil {
		t.Fatal(err)
	}
	self.pending.add(grantEvent)
	self.refreshPendingMessages()

	return screenOutput.String()
}

func drawPendingMarkdown(t *testing.T) string {
	t.Helper()

	var screenOutput strings.Builder
	screen := output.NewTerminalOfSize(&screenOutput, terminalInputColumns, replayLines)
	screen.Blank()
	screen.OpenNotice(painter.NewPendingMessages([]string{"# Heading\n\n- first item\n- second item"}, true))
	screen.Seal()

	return screenOutput.String()
}
