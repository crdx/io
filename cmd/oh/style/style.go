package style

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"crdx.org/col"

	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/cmd/oh/width"
)

// —————————————————————————————————————————————————————————————————————————————————————————————————
// mega:allow-file comment-inlines
// —————————————————————————————————————————————————————————————————————————————————————————————————

// Style renders text as one kind of line, and formats nothing when given no arguments, so a delta
// with a percent sign in it is safe to hand one.
type Style func(format any, args ...any) string

const (
	red       = "#cc6666"
	copper    = "#c08050"
	gold      = "#cfad00"
	maize     = "#f0c674"
	sage      = "#b5bd68"
	lime      = "#4c9a2c"
	teal      = "#8abeb7"
	steel     = "#81a2be"
	mauve     = "#c9a6d4"
	lightGrey = "#969896"

	none = "" // whatever the terminal draws with, which is what donnie asks for as text
)

// The mapping of kind of line to colour.
var (
	Normal    Style = hex(none)                            // ordinary text in the terminal's foreground
	Reasoning Style = decorate(col.Italic, hex(lightGrey)) // what the model thought on the way to it
	Answer    Style = hex(none)                            // the model's reply
	Call      Style = hex(none)                            // the name of a call that changes nothing
	Change    Style = hex(gold)                            // the name of a call that may change something
	Success   Style = hex(lime)                            // the mark against a call that finished
	Cancelled Style = hex(lightGrey)                       // the name of a call stopped before it got anywhere
	Stopped   Style = hex(gold)                            // what the harness says of a turn it was told to stop
	Failure   Style = hex(red)                             // what went wrong
	Subject   Style = hex(copper)                          // the subject of a call
	Qualifier Style = hex(lightGrey)                       // what qualifies the subject
	Result    Style = hex(lightGrey)                       // what a call handed back
	Spinner   Style = hex(copper)                          // the spinner
	Prompt    Style = hex(copper)                          // the harness prompting the user for text
	Rule      Style = hex(lightGrey)                       // the line drawn over the input
	Subtle    Style = hex(lightGrey)                       // dimmed text, a step back from the subject
	Read      Style = hex(lime)                            // reading is on offer
	Write     Style = hex(gold)                            // writing is on offer
	Exec      Style = hex(red)                             // running a command is on offer
	Shell     Style = hex(steel)                           // a shell prompt, matching the command name
	Skill     Style = hex(mauve)                           // a skill being read
	History   Style = hex(mauve)                           // changing a repository's own history is on offer
	Pending   Style = col.Underline                        // waiting on the keypress that follows a prefix
	Scrolled  Style = hex(lightGrey)                       // how much of the input is out of sight
	Withheld  Style = decorate(col.Dim, hex(lightGrey))    // that access is not on offer
	Chosen    Style = hex(copper)                          // the row the cursor is on in a list
	Typed     Style = hex(none)                            // what was typed, when a stored conversation is replayed
	User      Style = background("#343541")                // a submitted message, set apart from the model's reply
)

// Background marks permission to leave processes behind.
var Background Style = hex(teal)

// The markdown of an answer.
var (
	Heading Style = hex(gold)      // a heading, which is drawn in bold as well
	Link    Style = hex(steel)     // what a link says
	Address Style = hex(lightGrey) // where it goes
	Code    Style = hex(copper)    // code within a line
	Block   Style = hex(lightGrey) // code on lines of its own, where nothing highlights it
	Quote   Style = hex(lightGrey) // what is quoted
	Bullet  Style = hex(copper)    // what a list item is marked with
	Border  Style = hex(lightGrey) // a rule, a table's borders, and the bar down a quote
)

var (
	Comment     Style = hex(lightGrey)
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
	Inserted Style = hex(lime)
	Deleted  Style = hex(red)
	Hunk     Style = hex(steel)
)

var colorEnabled = true

// Init decides whether anything is painted at all.
func Init(screen any) func() {
	previous := colorEnabled

	apply(os.Getenv("NO_COLOR") == "" && tty.Is(screen))

	return func() {
		apply(previous)
	}
}

func apply(enabled bool) {
	colorEnabled = enabled

	if enabled {
		col.Enable()
	} else {
		col.Disable()
	}
}

// Width is how many cells text takes up once painted.
func Width(text string) int {
	return width.Of(Plain(text))
}

// Plain is text with the colour escape sequences taken out.
func Plain(text string) string {
	var out strings.Builder

	escaped := false

	for _, value := range text {
		switch {
		case escaped && (value == 'm' || value == 'K'):
			escaped = false
		case escaped:
		case value == '\x1b':
			escaped = true
		default:
			out.WriteRune(value)
		}
	}

	return out.String()
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

		if code == "" || !colorEnabled {
			return text
		}

		return "\x1b[" + code + "m" + text + "\x1b[0m"
	}
}

func background(value string) Style {
	code := strings.Replace(sgr(value), "38;", "48;", 1)

	return func(format any, args ...any) string {
		text := fmt.Sprint(format)

		if len(args) > 0 {
			text = fmt.Sprintf(text, args...)
		}

		if code == "" || !colorEnabled {
			return text
		}

		openingSequence := "\x1b[" + code + "m"

		text = strings.ReplaceAll(text, "\x1b[0m", "\x1b[0m"+openingSequence)

		return openingSequence + text + "\x1b[0m"
	}
}

func sgr(value string) string {
	if len(value) != len("#rrggbb") || value[0] != '#' {
		return "" // not a colour, and so drawn in whatever the terminal draws with
	}

	channels := make([]uint64, 3)

	for i := range channels {
		at := 1 + i*2

		channelValue, err := strconv.ParseUint(value[at:at+2], 16, 8)
		if err != nil {
			return ""
		}

		channels[i] = channelValue
	}

	return fmt.Sprintf("38;2;%d;%d;%d", channels[0], channels[1], channels[2])
}
