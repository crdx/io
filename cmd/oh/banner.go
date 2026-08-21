package main

import (
	"fmt"
	"strings"

	"crdx.org/io/cmd/oh/spinner"
	"crdx.org/io/cmd/oh/style"
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
	activity := style.Withheld("✧·")
	if running {
		activity = style.Spinner(spinner.Activity.Frame(frame))
	}

	parts := []string{
		activity,
		modes(tools, shell, history, background, pending),
		style.Subtle(pathutil.Abbr(directory)),
		style.Subtle(model),
		style.Subtle(short(effort)),
	}

	return strings.Join(parts, " "+style.Subtle("─")+" ")
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

	return style.Subtle(fmt.Sprintf("%d%%", percentage))
}

func rule(width int, left string, right string) string {
	return styledRule(width, left, style.Scrolled, right, style.Subtle)
}

func bannerRule(width int, banner string, scrolled string) string {
	if labelWidth(banner, leadingPadding)+labelWidth(scrolled, trailingPadding) > width {
		scrolled = ""
	}

	return styledRule(width, banner, nil, scrolled, style.Scrolled)
}

func styledRule(
	width int,
	left string,
	leftStyle style.Style,
	right string,
	rightStyle style.Style,
) string {
	head := ""

	if cells := labelWidth(left, leadingPadding); cells > 0 && cells+labelWidth(right, trailingPadding) <= width {
		if leftStyle != nil {
			left = leftStyle(left)
		}

		head = style.Rule(strings.Repeat("─", leadingPadding)) + " " + left + " "
		width -= cells
	}

	return head + ruleTo(width, right, rightStyle)
}

func labelWidth(label string, edgePadding int) int {
	if label == "" {
		return 0
	}

	return style.Width(label) + edgePadding + 2
}

func ruleTo(width int, label string, paint style.Style) string {
	cells := labelWidth(label, trailingPadding)
	if cells == 0 || cells > width {
		return style.Rule(strings.Repeat("─", max(width, 0)))
	}

	return style.Rule(strings.Repeat("─", width-cells)) +
		" " + paint(label) + " " +
		style.Rule(strings.Repeat("─", trailingPadding))
}

func modes(tools []tool.Tool, shell bool, history bool, background bool, pending bool) string {
	reads, writes := separateTools(tools)

	return offeredCapability(capRead.flag(), reads > 0, style.Read, pending) +
		offeredCapability(capShell.flag(), shell, style.Exec, pending) +
		offeredCapability(capWrite.flag(), writes > 0, style.Write, pending) +
		offeredCapability(capGit.flag(), history, style.History, pending) +
		offeredCapability(capBackground.flag(), background, style.Background, pending)
}

func offeredCapability(flag string, given bool, paint style.Style, pending bool) string {
	if !given {
		paint = style.Withheld
	}

	if pending {
		return style.Pending(paint(flag))
	}

	return paint(flag)
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
