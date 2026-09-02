package style

import (
	"fmt"
	"image/color"
	"os"
	"strconv"
	"strings"

	"crdx.org/col"

	"crdx.org/io/cmd/oh/escape"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/internal/util"
)

type Style func(format any, args ...any) string

const (
	red    = "#cc6666"
	copper = "#c08050"
	gold   = "#cfad00"
	maize  = "#f0c674"
	sage   = "#b5bd68"
	lime   = "#4c9a2c"
	teal   = "#8abeb7"
	steel  = "#81a2be"
	mauve  = "#c9a6d4"
	grey   = "#969896"

	none = ""
)

const reset = "\x1b[0m"

var (
	Accent Style = hex(copper)
	Normal Style = hex(none)
	Dim    Style = hex(grey)
)

var (
	Reasoning     Style = decorate(col.Italic, Dim)
	Answer        Style = Normal
	Call          Style = Normal
	Change        Style = hex(gold)
	Success       Style = hex(lime)
	Information   Style = hex(steel)
	CancelledCall Style = Dim
	StoppedTurn   Style = hex(gold)
	Failure       Style = hex(red)
	Subject       Style = hex(copper)
	Qualifier     Style = Dim
	Result        Style = Dim
	Spinner       Style = hex(copper)
	Prompt        Style = hex(copper)
	Rule          Style = Dim
	Hazard        Style = hex(red)
	Subtle        Style = Dim
	Read          Style = hex(lime)
	Write         Style = hex(gold)
	Exec          Style = hex(red)
	Shell         Style = hex(steel)
	Skill         Style = hex(mauve)
	History       Style = hex(mauve)
	Pending       Style = col.Underline
	ScrolledInput Style = Dim
	ChosenRow     Style = hex(copper)
	Running       Style = decorate(col.Italic, Dim)
	Column        Style = decorate(col.Underline, Dim)
	TypedInput    Style = Normal
	User          Style = background("#343541")
	Greeting      Style = col.Italic
	Web           Style = hex(steel)
	Network       Style = hex(red)
)

var (
	Heading Style = hex(gold)
	Link    Style = hex(steel)
	Address Style = Dim
	Code    Style = hex(copper)
	Block   Style = Dim
	Quote   Style = Dim
	Bullet  Style = hex(copper)
	Border  Style = Dim
)

var (
	Comment     Style = Dim
	Keyword     Style = hex(mauve)
	Function    Style = hex(steel)
	Literal     Style = hex(sage)
	Number      Style = hex(copper)
	Type        Style = hex(maize)
	Operator    Style = hex(teal)
	Variable    Style = hex(none)
	Punctuation Style = hex(none)
)

var (
	InformationColour, _ = colour(steel)
	ChangeColour, _      = colour(gold)
	FailureColour, _     = colour(red)
	DimColour, _         = colour(grey)
)

var (
	InsertedText Style = hex(lime)
	DeletedText  Style = hex(red)
	Hunk         Style = hex(steel)
)

var isColorEnabled = true

func Init(screen any) func() {
	previous := isColorEnabled

	apply(os.Getenv("NO_COLOR") == "" && tty.Is(screen))

	return func() {
		apply(previous)
	}
}

func apply(isEnabled bool) {
	isColorEnabled = isEnabled

	if isEnabled {
		col.Enable()
	} else {
		col.Disable()
	}
}

func (self Style) Over(text string) string {
	const marker = "\x00"

	opening, closing, found := strings.Cut(self(marker), marker)
	if !found || opening == "" {
		return text
	}

	resumedText := strings.TrimSuffix(strings.ReplaceAll(text, reset, reset+opening), opening)
	if strings.HasSuffix(resumedText, closing) {
		return opening + resumedText
	}

	return opening + resumedText + closing
}

func (self Style) Join(parts ...string) string {
	joinedText := util.JoinNonEmpty(parts...)
	if joinedText == "" {
		return ""
	}

	return self(joinedText)
}

func Width(text string) int {
	return width.Of(text)
}

func Plain(text string) string {
	var out strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); {
		if runes[i] == '\x1b' {
			sequence := escape.GetSequence(runes, i)
			out.WriteString(sequence.Text)
			i = sequence.End
			continue
		}

		out.WriteRune(runes[i])
		i++
	}

	return out.String()
}

func Quantity(text string) string {
	var out strings.Builder

	for start := 0; start < len(text); {
		end := start + 1
		for end < len(text) && isNumeric(text[end]) == isNumeric(text[start]) {
			end++
		}

		run := text[start:end]
		if isNumeric(text[start]) {
			out.WriteString(Normal(run))
		} else {
			out.WriteString(Subtle(run))
		}

		start = end
	}

	return out.String()
}

func isNumeric(character byte) bool {
	return character >= '0' && character <= '9' || character == '.' || character == '?'
}

func decorate(decoration Style, inner Style) Style {
	return func(format any, args ...any) string {
		return decoration(inner(format, args...))
	}
}

func hex(value string) Style {
	code := sgr(value)

	return func(format any, args ...any) string {
		text := fmt.Sprint(format)

		if len(args) > 0 {
			text = fmt.Sprintf(text, args...)
		}

		if code == "" || !isColorEnabled {
			return text
		}

		return "\x1b[" + code + "m" + text + reset
	}
}

func background(value string) Style {
	code := strings.Replace(sgr(value), "38;", "48;", 1)

	return func(format any, args ...any) string {
		text := fmt.Sprint(format)

		if len(args) > 0 {
			text = fmt.Sprintf(text, args...)
		}

		if code == "" || !isColorEnabled {
			return text
		}

		openingSequence := "\x1b[" + code + "m"

		text = strings.ReplaceAll(text, reset, reset+openingSequence)

		return openingSequence + text + reset
	}
}

func sgr(value string) string {
	colourValue, isColour := colour(value)
	if !isColour {
		return ""
	}

	return fmt.Sprintf("38;2;%d;%d;%d", colourValue.R, colourValue.G, colourValue.B)
}

func colour(value string) (color.RGBA, bool) {
	if len(value) != len("#rrggbb") || value[0] != '#' {
		return color.RGBA{}, false
	}

	colourValue := color.RGBA{A: 0xff}

	for i, channel := range []*uint8{&colourValue.R, &colourValue.G, &colourValue.B} {
		at := 1 + i*2

		channelValue, err := strconv.ParseUint(value[at:at+2], 16, 8)
		if err != nil {
			return color.RGBA{}, false
		}

		*channel = uint8(channelValue)
	}

	return colourValue, true
}
