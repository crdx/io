package main

import (
	"fmt"
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
	activity := theme.Withheld("✧·")
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

const (
	contextWindowTokens = 274_000
	leadingPadding      = 1
	trailingPadding     = 2
)

func contextUsage(inputTokens int) string {
	if inputTokens <= 0 {
		return ""
	}

	percentage := min(100, (inputTokens*100+contextWindowTokens/2)/contextWindowTokens)

	return theme.Subtle(fmt.Sprintf("%d%%", percentage))
}

func rule(width int, left string, right string) string {
	return styledRule(width, left, theme.Scrolled, right, theme.Subtle)
}

func bannerRule(width int, banner string, scrolled string) string {
	if labelWidth(banner, leadingPadding)+labelWidth(scrolled, trailingPadding) > width {
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

	if cells := labelWidth(left, leadingPadding); cells > 0 && cells+labelWidth(right, trailingPadding) <= width {
		if leftStyle != nil {
			left = leftStyle(left)
		}

		head = theme.Rule(strings.Repeat("─", leadingPadding)) + " " + left + " "
		width -= cells
	}

	return head + ruleTo(width, right, rightStyle)
}

func labelWidth(label string, edgePadding int) int {
	if label == "" {
		return 0
	}

	return theme.Width(label) + edgePadding + 2
}

func ruleTo(width int, label string, style theme.Style) string {
	cells := labelWidth(label, trailingPadding)
	if cells == 0 || cells > width {
		return theme.Rule(strings.Repeat("─", max(width, 0)))
	}

	return theme.Rule(strings.Repeat("─", width-cells)) +
		" " + style(label) + " " +
		theme.Rule(strings.Repeat("─", trailingPadding))
}

func modes(tools []tool.Tool, shell bool, history bool, background bool, pending bool) string {
	reads, writes := separateTools(tools)

	return offeredCapability(capRead.flag(), reads > 0, theme.Read, pending) +
		offeredCapability(capWrite.flag(), writes > 0, theme.Write, pending) +
		offeredCapability(capShell.flag(), shell, theme.Exec, pending) +
		offeredCapability(capGit.flag(), history, theme.History, pending) +
		offeredCapability(capBackground.flag(), background, theme.Background, pending)
}

func offeredCapability(flag string, given bool, style theme.Style, pending bool) string {
	if !given {
		style = theme.Withheld
	}

	if pending { // per flag: each ends in a reset that would end an underline drawn over the lot
		return theme.Pending(style(flag))
	}

	return style(flag)
}

func separateTools(tools []tool.Tool) (int, int) {
	var reads int
	var writes int

	for _, offeredTool := range tools {
		if offeredTool.ReadOnly() {
			reads++
		} else {
			writes++
		}
	}

	return reads, writes
}
