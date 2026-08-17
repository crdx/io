package main

import (
	"strings"

	"crdx.org/io/cmd/oh/spinner"
	"crdx.org/io/cmd/oh/theme"
	"crdx.org/io/internal/pathutil"
	"crdx.org/io/tool"
)

func banner(
	model string,
	effort string,
	directory string,
	tools []tool.Tool,
	shell bool,
	history bool,
	background bool,
	pending bool,
	frame int,
	running bool,
) string {
	activity := theme.Withheld("⠶")
	if running {
		activity = theme.Spinner(spinner.Activity.Frame(frame))
	}

	parts := []string{
		activity,
		modes(tools, shell, history, background, pending),
		theme.Subtle(pathutil.Abbr(directory)),
		theme.Subtle(model),
		theme.Subtle(short(effort)),
	}

	return strings.Join(parts, " "+theme.Subtle("─")+" ")
}

func short(effort string) string {
	switch effort {
	case "minimal":
		return "min"
	case "medium":
		return "med"
	default:
		return effort
	}
}

const padding = 2

func rule(width int, left string, right string) string {
	return styledRule(width, left, theme.Scrolled, right, theme.Subtle)
}

func bannerRule(width int, banner string, scrolled string) string {
	if labelWidth(banner)+labelWidth(scrolled) > width {
		scrolled = ""
	}

	return styledRule(width, banner, nil, scrolled, theme.Scrolled)
}

func styledRule(
	width int,
	left string,
	leftStyle theme.Style,
	right string,
	rightStyle theme.Style,
) string {
	head := ""

	if cells := labelWidth(left); cells > 0 && cells+labelWidth(right) <= width {
		if leftStyle != nil {
			left = leftStyle(left)
		}

		head = theme.Rule(strings.Repeat("─", padding)) + " " + left + " "
		width -= cells
	}

	return head + ruleTo(width, right, rightStyle)
}

func labelWidth(label string) int {
	if label == "" {
		return 0
	}

	return theme.Width(label) + padding + 2
}

func ruleTo(width int, label string, style theme.Style) string {
	cells := labelWidth(label)
	if cells == 0 || cells > width {
		return theme.Rule(strings.Repeat("─", max(width, 0)))
	}

	return theme.Rule(strings.Repeat("─", width-cells)) +
		" " + style(label) + " " +
		theme.Rule(strings.Repeat("─", padding))
}

func modes(tools []tool.Tool, shell bool, history bool, background bool, pending bool) string {
	reads, writes := toolMix(tools)

	return offeredCapability(capRead.letter(), reads > 0, theme.Read, pending) +
		offeredCapability(capWrite.letter(), writes > 0, theme.Write, pending) +
		offeredCapability(capShell.letter(), shell, theme.Exec, pending) +
		offeredCapability(capGit.letter(), history, theme.History, pending) +
		offeredCapability(capBackground.letter(), background, theme.Background, pending)
}

func offeredCapability(letter string, given bool, style theme.Style, pending bool) string {
	if !given {
		style = theme.Withheld
	}

	if pending { // per letter: each ends in a reset that would end an underline drawn over the lot
		return theme.Pending(style(letter))
	}

	return style(letter)
}

func toolMix(tools []tool.Tool) (reads int, writes int) {
	for _, offeredTool := range tools {
		if offeredTool.ReadOnly() {
			reads++
		} else {
			writes++
		}
	}

	return reads, writes
}
