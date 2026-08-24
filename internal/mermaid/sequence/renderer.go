package sequence

import (
	"fmt"
	"strings"

	"crdx.org/io/internal/mermaid/diagram"
	"crdx.org/io/internal/mermaid/runewidth"
)

const (
	defaultSelfMessageWidth   = 4
	defaultMessageSpacing     = 1
	defaultParticipantSpacing = 5
	boxPaddingLeftRight       = 2
	minBoxWidth               = 3
	boxBorderWidth            = 2
	labelLeftMargin           = 2
	labelBufferSpace          = 10
	frameIndent               = 2
	frameLabelInset           = 2
)

type diagramLayout struct {
	participantWidths  []int
	participantCenters []int
	totalWidth         int
	messageSpacing     int
	selfMessageWidth   int
}

func calculateLayout(sd *SequenceDiagram, config *diagram.Config) *diagramLayout {
	participantSpacing := config.SequenceParticipantSpacing
	if participantSpacing <= 0 {
		participantSpacing = defaultParticipantSpacing
	}

	widths := make([]int, len(sd.Participants))
	for i, p := range sd.Participants {
		w := max(runewidth.StringWidth(p.Label)+boxPaddingLeftRight, minBoxWidth)
		widths[i] = w
	}

	centers := make([]int, len(sd.Participants))
	currentX := 0
	for i := range sd.Participants {
		boxWidth := widths[i] + boxBorderWidth
		if i == 0 {
			centers[i] = boxWidth / 2
			currentX = boxWidth
		} else {
			currentX += participantSpacing
			centers[i] = currentX + boxWidth/2
			currentX += boxWidth
		}
	}

	last := len(sd.Participants) - 1
	totalWidth := centers[last] + (widths[last]+boxBorderWidth)/2

	msgSpacing := config.SequenceMessageSpacing
	if msgSpacing <= 0 {
		msgSpacing = defaultMessageSpacing
	}
	selfWidth := config.SequenceSelfMessageWidth
	if selfWidth <= 0 {
		selfWidth = defaultSelfMessageWidth
	}

	return &diagramLayout{
		participantWidths:  widths,
		participantCenters: centers,
		totalWidth:         totalWidth,
		messageSpacing:     msgSpacing,
		selfMessageWidth:   selfWidth,
	}
}

func Render(sd *SequenceDiagram, config *diagram.Config) (string, error) {
	if sd == nil || len(sd.Participants) == 0 {
		return "", fmt.Errorf("no participants")
	}
	if config == nil {
		config = diagram.DefaultConfig()
	}

	chars := Unicode
	if config.UseAscii {
		chars = ASCII
	}

	layout := calculateLayout(sd, config)

	events := sd.Events
	if len(events) == 0 {
		for _, msg := range sd.Messages {
			events = append(events, Event{Kind: EventMessage, Message: msg})
		}
	}

	if gutter := (fragmentDepth(events) - 1) * frameIndent; gutter > 0 {
		shiftLayoutRight(layout, gutter)
	}

	if gutter := noteLeftGutter(events, layout); gutter > 0 {
		shiftLayoutRight(layout, gutter)
	}

	var lines []string

	lines = append(lines, buildLine(sd.Participants, layout, func(i int) string {
		return string(chars.TopLeft) + strings.Repeat(string(chars.Horizontal), layout.participantWidths[i]) + string(chars.TopRight)
	}))

	lines = append(lines, buildLine(sd.Participants, layout, func(i int) string {
		w := layout.participantWidths[i]
		labelLen := runewidth.StringWidth(sd.Participants[i].Label)
		pad := (w - labelLen) / 2
		return string(chars.Vertical) + strings.Repeat(" ", pad) + sd.Participants[i].Label +
			strings.Repeat(" ", w-pad-labelLen) + string(chars.Vertical)
	}))

	lines = append(lines, buildLine(sd.Participants, layout, func(i int) string {
		w := layout.participantWidths[i]
		return string(chars.BottomLeft) + strings.Repeat(string(chars.Horizontal), w/2) +
			string(chars.TeeDown) + strings.Repeat(string(chars.Horizontal), w-w/2-1) +
			string(chars.BottomRight)
	}))

	lines = append(lines, renderEvents(events, layout, chars)...)

	lines = append(lines, buildLifeline(layout, chars))
	return strings.Join(lines, "\n") + "\n", nil
}

func renderEvents(events []Event, layout *diagramLayout, chars BoxChars) []string {
	var lines []string
	for i := 0; i < len(events); {
		ev := events[i]
		if ev.Kind == EventFragmentStart {
			end := matchingFragmentEnd(events, i)
			lines = append(lines, wrapFragment(ev.Fragment, events[i+1:end], layout, chars)...)
			i = end + 1
			continue
		}

		if ev.Kind == EventFragmentEnd || ev.Kind == EventFragmentDivider {
			i++
			continue
		}

		if ev.Kind == EventNote {
			for range layout.messageSpacing {
				lines = append(lines, buildLifeline(layout, chars))
			}
			lines = append(lines, renderNote(ev.Note, layout, chars)...)
			i++
			continue
		}

		msg := ev.Message
		for range layout.messageSpacing {
			lines = append(lines, buildLifeline(layout, chars))
		}
		if msg.From == msg.To {
			lines = append(lines, renderSelfMessage(msg, layout, chars)...)
		} else {
			lines = append(lines, renderMessage(msg, layout, chars)...)
		}
		i++
	}
	return lines
}

func fragmentDepth(events []Event) int {
	maxDepth, cur := 0, 0
	for _, ev := range events {
		switch ev.Kind {
		case EventFragmentStart:
			cur++
			if cur > maxDepth {
				maxDepth = cur
			}
		case EventFragmentEnd:
			cur--
		}
	}
	return maxDepth
}

func matchingFragmentEnd(events []Event, start int) int {
	depth := 0
	for i := start; i < len(events); i++ {
		switch events[i].Kind {
		case EventFragmentStart:
			depth++
		case EventFragmentEnd:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(events)
}

func shiftLayoutRight(layout *diagramLayout, n int) {
	for i := range layout.participantCenters {
		layout.participantCenters[i] += n
	}
	layout.totalWidth += n
}

func noteLeftGutter(events []Event, layout *diagramLayout) int {
	gutter, depth := 0, 0
	for _, ev := range events {
		switch ev.Kind {
		case EventFragmentStart:
			depth++
		case EventFragmentEnd:
			depth--
		case EventNote:
			left, _ := noteBoxColumns(ev.Note, layout)
			extra := 0
			if depth > 0 {
				extra = 1 + (depth-1)*frameIndent
			}
			if need := -left + extra; need > gutter {
				gutter = need
			}
		}
	}
	return gutter
}

func noteRunes(note *Note) []rune {
	text := note.Text
	for _, br := range []string{"<br/>", "<br />", "<br>"} {
		text = strings.ReplaceAll(text, br, " ")
	}
	return []rune(text)
}

func noteBoxColumns(note *Note, layout *diagramLayout) (int, int) {
	boxW := len(noteRunes(note)) + 4
	centers := layout.participantCenters
	first := centers[note.Participants[0].Index]
	last := centers[note.Participants[len(note.Participants)-1].Index]

	switch note.Placement {
	case NoteRightOf:
		left := first + 2
		return left, left + boxW - 1
	case NoteLeftOf:
		right := first - 2
		return right - boxW + 1, right
	default:
		lo, hi := first, last
		if hi < lo {
			lo, hi = hi, lo
		}
		left, right := lo-1, hi+1
		if extra := boxW - (right - left + 1); extra > 0 {
			left -= extra / 2
			right += extra - extra/2
		}
		return left, right
	}
}

func renderNote(note *Note, layout *diagramLayout, chars BoxChars) []string {
	runes := noteRunes(note)
	left, right := noteBoxColumns(note, layout)
	if left < 0 {
		left = 0
	}

	border := func(l, r rune) string {
		line := padRunes(buildLifeline(layout, chars), right+1)
		line[left] = l
		for c := left + 1; c < right; c++ {
			line[c] = chars.Horizontal
		}
		line[right] = r
		return strings.TrimRight(string(line), " ")
	}

	mid := padRunes(buildLifeline(layout, chars), right+1)
	for c := left; c <= right; c++ {
		mid[c] = ' '
	}
	mid[left] = chars.Vertical
	mid[right] = chars.Vertical
	inner := right - left - 1
	col := left + 1 + (inner-len(runes))/2
	for _, ch := range runes {
		if col > left && col < right {
			mid[col] = ch
		}
		col++
	}

	return []string{
		border(chars.TopLeft, chars.TopRight),
		strings.TrimRight(string(mid), " "),
		border(chars.BottomLeft, chars.BottomRight),
	}
}

func wrapFragment(frag *Fragment, inner []Event, layout *diagramLayout, chars BoxChars) []string {
	sections, dividerLabels := splitSections(inner)
	var body []string
	dividerAt := map[int]string{}
	for i, sec := range sections {
		if i > 0 {
			dividerAt[len(body)] = dividerLabels[i-1]
			body = append(body, "")
		}
		body = append(body, renderEvents(sec, layout, chars)...)
	}
	body = append(body, buildLifeline(layout, chars))

	leftIdx, rightIdx := involvedParticipants(inner, layout)
	leftCol := layout.participantCenters[leftIdx] - frameIndent*(fragmentDepth(inner)+1)
	rightCol := layout.participantCenters[rightIdx] + frameIndent

	rd := 0
	for _, ev := range inner {
		switch ev.Kind {
		case EventFragmentStart:
			rd++
		case EventFragmentEnd:
			rd--
		case EventNote:
			nl, nr := noteBoxColumns(ev.Note, layout)
			if x := nl - 1 - rd*frameIndent; x < leftCol {
				leftCol = x
			}
			if x := nr + 1 + rd*frameIndent; x > rightCol {
				rightCol = x
			}
		}
	}
	if leftCol < 0 {
		leftCol = 0
	}

	for _, l := range body {
		if w := len([]rune(l)) + 1; w > rightCol {
			rightCol = w
		}
	}

	label := frag.Type.String()
	if frag.Label != "" {
		label += " " + frag.Label
	}

	widen := func(text string) {
		if end := leftCol + frameLabelInset + len([]rune("["+text+"]")) + 1; end > rightCol {
			rightCol = end
		}
	}
	widen(label)
	for _, l := range dividerLabels {
		if l != "" {
			widen(l)
		}
	}

	out := []string{fragmentBorder(layout, chars, leftCol, rightCol, label, true)}
	for idx, l := range body {
		if dl, ok := dividerAt[idx]; ok {
			out = append(out, fragmentDivider(layout, chars, leftCol, rightCol, dl))
		} else {
			out = append(out, overlayFrameSides(l, chars, leftCol, rightCol))
		}
	}
	out = append(out, fragmentBorder(layout, chars, leftCol, rightCol, "", false))
	return out
}

func splitSections(inner []Event) ([][]Event, []string) {
	var sections [][]Event
	var labels []string
	cur := []Event{}
	depth := 0
	for _, ev := range inner {
		switch ev.Kind {
		case EventFragmentStart:
			depth++
		case EventFragmentEnd:
			depth--
		case EventFragmentDivider:
			if depth == 0 {
				sections = append(sections, cur)
				labels = append(labels, ev.Fragment.Label)
				cur = []Event{}
				continue
			}
		}
		cur = append(cur, ev)
	}
	return append(sections, cur), labels
}

func fragmentDivider(layout *diagramLayout, chars BoxChars, leftCol, rightCol int, label string) string {
	line := padRunes(buildLifeline(layout, chars), rightCol+1)
	line[leftCol] = chars.TeeRight
	for c := leftCol + 1; c < rightCol; c++ {
		line[c] = chars.DottedLine
	}
	line[rightCol] = chars.TeeLeft
	if label != "" {
		col := leftCol + frameLabelInset
		for _, r := range "[" + label + "]" {
			if col < rightCol {
				line[col] = r
				col++
			}
		}
	}
	return strings.TrimRight(string(line), " ")
}

func involvedParticipants(events []Event, layout *diagramLayout) (int, int) {
	minIdx, maxIdx := -1, -1
	note := func(idx int) {
		if minIdx == -1 || idx < minIdx {
			minIdx = idx
		}
		if maxIdx == -1 || idx > maxIdx {
			maxIdx = idx
		}
	}
	for _, ev := range events {
		if ev.Kind == EventMessage {
			note(ev.Message.From.Index)
			note(ev.Message.To.Index)
		}
	}
	if minIdx == -1 {
		return 0, len(layout.participantCenters) - 1
	}
	return minIdx, maxIdx
}

func fragmentBorder(layout *diagramLayout, chars BoxChars, leftCol, rightCol int, label string, top bool) string {
	line := padRunes(buildLifeline(layout, chars), rightCol+1)

	leftCorner, rightCorner := chars.BottomLeft, chars.BottomRight
	if top {
		leftCorner, rightCorner = chars.TopLeft, chars.TopRight
	}
	line[leftCol] = leftCorner
	for c := leftCol + 1; c < rightCol; c++ {
		line[c] = chars.Horizontal
	}
	line[rightCol] = rightCorner

	if label != "" {
		col := leftCol + frameLabelInset
		for _, r := range "[" + label + "]" {
			if col < rightCol {
				line[col] = r
				col++
			}
		}
	}
	return strings.TrimRight(string(line), " ")
}

func overlayFrameSides(line string, chars BoxChars, leftCol, rightCol int) string {
	r := padRunes(line, rightCol+1)
	r[leftCol] = chars.Vertical
	r[rightCol] = chars.Vertical
	return strings.TrimRight(string(r), " ")
}

func padRunes(s string, width int) []rune {
	r := []rune(s)
	for len(r) < width {
		r = append(r, ' ')
	}
	return r
}

func buildLine(participants []*Participant, layout *diagramLayout, draw func(int) string) string {
	var sb strings.Builder
	for i := range participants {
		boxWidth := layout.participantWidths[i] + boxBorderWidth
		left := layout.participantCenters[i] - boxWidth/2

		needed := left - len([]rune(sb.String()))
		if needed > 0 {
			sb.WriteString(strings.Repeat(" ", needed))
		}
		sb.WriteString(draw(i))
	}
	return sb.String()
}

func buildLifeline(layout *diagramLayout, chars BoxChars) string {
	line := make([]rune, layout.totalWidth+1)
	for i := range line {
		line[i] = ' '
	}
	for _, c := range layout.participantCenters {
		if c < len(line) {
			line[c] = chars.Vertical
		}
	}
	return strings.TrimRight(string(line), " ")
}

func renderMessage(msg *Message, layout *diagramLayout, chars BoxChars) []string {
	var lines []string
	from, to := layout.participantCenters[msg.From.Index], layout.participantCenters[msg.To.Index]

	label := msg.Label
	if msg.Number > 0 {
		label = fmt.Sprintf("%d. %s", msg.Number, msg.Label)
	}

	if label != "" {
		start := min(from, to) + labelLeftMargin
		labelWidth := runewidth.StringWidth(label)
		w := max(layout.totalWidth, start+labelWidth) + labelBufferSpace
		line := []rune(buildLifeline(layout, chars))
		if len(line) < w {
			padding := make([]rune, w-len(line))
			for k := range padding {
				padding[k] = ' '
			}
			line = append(line, padding...)
		}

		col := start
		for _, r := range label {
			if col < len(line) {
				line[col] = r
				col++
			}
		}
		lines = append(lines, strings.TrimRight(string(line), " "))
	}

	line := []rune(buildLifeline(layout, chars))
	style := chars.SolidLine
	if msg.ArrowType.isDotted() {
		style = chars.DottedLine
	}

	if from < to {
		line[from] = chars.TeeRight
		for i := from + 1; i < to; i++ {
			line[i] = style
		}
		if head, ok := msg.ArrowType.head(chars, true); ok {
			line[to-1] = head
		}
		if msg.ArrowType.isBidirectional() {
			line[from+1] = chars.ArrowLeft
		}
		line[to] = chars.Vertical
	} else {
		line[to] = chars.Vertical
		line[to+1] = style
		if head, ok := msg.ArrowType.head(chars, false); ok {
			line[to+1] = head
		}
		for i := to + 2; i < from; i++ {
			line[i] = style
		}
		if msg.ArrowType.isBidirectional() {
			line[from-1] = chars.ArrowRight
		}
		line[from] = chars.TeeLeft
	}
	if msg.CentralFrom {
		line[from] = chars.Circle
	}
	if msg.CentralTo {
		line[to] = chars.Circle
	}
	lines = append(lines, strings.TrimRight(string(line), " "))
	return lines
}

func renderSelfMessage(msg *Message, layout *diagramLayout, chars BoxChars) []string {
	var lines []string
	center := layout.participantCenters[msg.From.Index]
	width := layout.selfMessageWidth

	ensureWidth := func(l string) []rune {
		target := layout.totalWidth + width + 1
		r := []rune(l)
		if len(r) < target {
			pad := make([]rune, target-len(r))
			for i := range pad {
				pad[i] = ' '
			}
			r = append(r, pad...)
		}
		return r
	}

	label := msg.Label
	if msg.Number > 0 {
		label = fmt.Sprintf("%d. %s", msg.Number, msg.Label)
	}

	if label != "" {
		line := ensureWidth(buildLifeline(layout, chars))
		start := center + labelLeftMargin
		labelWidth := runewidth.StringWidth(label)
		needed := start + labelWidth + labelBufferSpace
		if len(line) < needed {
			pad := make([]rune, needed-len(line))
			for i := range pad {
				pad[i] = ' '
			}
			line = append(line, pad...)
		}
		col := start
		for _, c := range label {
			if col < len(line) {
				line[col] = c
				col++
			}
		}
		lines = append(lines, strings.TrimRight(string(line), " "))
	}

	style := chars.Horizontal
	if msg.ArrowType.isDotted() {
		style = chars.DottedLine
	}

	l1 := ensureWidth(buildLifeline(layout, chars))
	l1[center] = chars.TeeRight
	if msg.CentralFrom {
		l1[center] = chars.Circle
	}
	for i := 1; i < width; i++ {
		l1[center+i] = style
	}
	l1[center+width-1] = chars.SelfTopRight
	lines = append(lines, strings.TrimRight(string(l1), " "))

	l2 := ensureWidth(buildLifeline(layout, chars))
	l2[center+width-1] = chars.Vertical
	lines = append(lines, strings.TrimRight(string(l2), " "))

	l3 := ensureWidth(buildLifeline(layout, chars))
	l3[center] = chars.Vertical
	if msg.CentralTo {
		l3[center] = chars.Circle
	}
	l3[center+1] = style
	if head, ok := msg.ArrowType.head(chars, false); ok {
		l3[center+1] = head
	}
	for i := 2; i < width-1; i++ {
		l3[center+i] = style
	}
	l3[center+width-1] = chars.SelfBottom
	lines = append(lines, strings.TrimRight(string(l3), " "))

	return lines
}
