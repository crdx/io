package painter

import (
	"strings"
	"time"
	"unicode"

	"crdx.org/io/agent"
	"crdx.org/io/ask"
	"crdx.org/io/cmd/oh/call"
	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/internal/util"
)

const (
	questionMark  = "⚠"
	detailGutter  = "❯"
	optionGap     = " "
	headGap       = " "
	lapseFallback = "auto-cancels"
)

func RenderQuestion(question ask.Question, cursor int, columns int) []string {
	rows := renderQuestionLabel(question.Label, columns)

	if detail := renderQuestionDetail(question, columns); len(detail) > 0 {
		rows = append(rows, "")
		rows = append(rows, detail...)
	}

	return append(rows, "", renderOptions(question, cursor, columns))
}

func QuestionHead(question ask.Question, remainingTime time.Duration) string {
	head := NoticeStyle(agent.WarningStatus).Over(questionMark)

	if countdown := renderCountdown(question.Lapse, remainingTime); countdown != "" {
		head += style.Subtle(headGap) + countdown
	}

	return head
}

func renderQuestionLabel(label string, columns int) []string {
	labelStyle := NoticeStyle(agent.WarningStatus)
	rows := width.Wrap(label, columns)

	for index, row := range rows {
		rows[index] = labelStyle.Over(row)
	}

	return rows
}

func renderQuestionDetail(question ask.Question, columns int) []string {
	if question.Detail == "" {
		return nil
	}

	detail := question.Detail
	if question.Language != "" {
		detail = markdown.Highlight(detail, detail, question.Language, false)
	}

	mark, markStyle := detailMark(question.Language)
	indent := strings.Repeat(" ", style.Width(mark)+1)
	gutter := markStyle(mark) + " "
	room := max(columns-style.Width(indent), 1)

	var rows []string
	for line := range strings.SplitSeq(detail, "\n") {
		for _, row := range width.Wrap(line, room) {
			rows = append(rows, gutter+row)
			gutter = indent
		}
	}

	return rows
}

func detailMark(language string) (string, style.Style) {
	if mark, markStyle := call.ToolMark(language); mark != "" {
		return mark, markStyle
	}

	return detailGutter, style.Subject
}

func renderOptions(question ask.Question, cursor int, columns int) string {
	labels := make([]string, 0, len(question.Options))

	for index, option := range question.Options {
		labels = append(labels, renderOption(option, index == cursor))
	}

	return width.Elide(strings.Join(labels, optionGap), columns)
}

func renderOption(option ask.Option, isChosen bool) string {
	label := labelledOption(option)

	if isChosen {
		return style.ChosenRow("[" + label + "]")
	}

	return style.Subtle(" " + label + " ")
}

func labelledOption(option ask.Option) string {
	if option.Key == 0 || isKeyLeading(option) {
		return option.Label
	}

	return string(option.Key) + " " + option.Label
}

func isKeyLeading(option ask.Option) bool {
	letters := []rune(option.Label)

	return len(letters) > 0 && unicode.ToLower(letters[0]) == unicode.ToLower(option.Key)
}

func renderCountdown(lapse string, remainingTime time.Duration) string {
	if remainingTime <= 0 {
		return ""
	}

	if lapse == "" {
		lapse = lapseFallback
	}

	countdown := (remainingTime + time.Second - 1).Truncate(time.Second)

	return style.Subtle(lapse + " in " + util.CompactDuration(countdown))
}
