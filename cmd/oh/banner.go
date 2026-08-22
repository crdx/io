package main

import (
	"slices"
	"strings"

	"crdx.org/io/cmd/oh/caps"
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
	grantedCaps caps.Set,
	isPending bool,
	isRunning bool,
	frameIndex int,
) string {
	activity := style.Withheld("✧·")
	if isRunning {
		activity = style.Spinner(spinner.Activity.Frame(frameIndex))
	}

	parts := []string{
		activity,
		modes(tools, grantedCaps, isPending),
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
	leadingPadding  = 1
	trailingPadding = 2
)

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

func modes(tools []tool.Tool, grantedCaps caps.Set, isPending bool) string {
	return offeredCapability(caps.Read, anyToolReadsOnly(tools), style.Read, isPending) +
		offeredCapability(caps.Shell, grantedCaps.Has(caps.Shell), style.Exec, isPending) +
		offeredCapability(caps.Write, grantedCaps.Has(caps.Write), style.Write, isPending) +
		offeredCapability(caps.Git, grantedCaps.Has(caps.Git), style.History, isPending) +
		offeredCapability(caps.Background, grantedCaps.Has(caps.Background), style.Background, isPending)
}

func offeredCapability(whichCap caps.Set, isGranted bool, paint style.Style, isPending bool) string {
	if !isGranted {
		paint = style.Withheld
	}

	if isPending {
		return style.Pending(paint(whichCap.Flag()))
	}

	return paint(whichCap.Flag())
}

func anyToolReadsOnly(tools []tool.Tool) bool {
	return slices.ContainsFunc(tools, tool.Tool.ReadOnly)
}
