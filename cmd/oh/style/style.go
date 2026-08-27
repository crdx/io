package style

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"crdx.org/col"

	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/internal/util"
)

// —————————————————————————————————————————————————————————————————————————————————————————————————
// mega:allow-file comment-inlines
// —————————————————————————————————————————————————————————————————————————————————————————————————

// Style renders text as one kind of line, and formats nothing when given no arguments, so a delta
// with a percent sign in it is safe to hand one.
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

// The base visual styles.
var (
	Normal Style = hex(none) // the terminal's foreground
	Dim    Style = hex(grey) // grey text
)

// The mapping of kind of line to colour.
var (
	Reasoning   Style = decorate(col.Italic, Dim) // what the model thought on the way to an answer
	Answer      Style = Normal                    // the reply the model gives after it has thought
	Call        Style = Normal                    // the name of a call that changes nothing at all
	Change      Style = hex(gold)                 // the name of a call that could change something
	Success     Style = hex(lime)                 // the mark set against a call that has completed
	Information Style = hex(steel)                // what the harness says when passing information
	Cancelled   Style = Dim                       // the name of a call stopped before it got going
	Stopped     Style = hex(gold)                 // what the harness says of a turn it had to stop
	Failure     Style = hex(red)                  // what went wrong, wherever the failure happened
	Subject     Style = hex(copper)               // the subject of a call, whatever it operates on
	Qualifier   Style = Dim                       // what qualifies the subject and narrows it down
	Result      Style = Dim                       // the output a call handed back when it finished
	Spinner     Style = hex(copper)               // the spinner that turns while a call is running
	Prompt      Style = hex(copper)               // the harness prompting the user to enter a line
	Rule        Style = Dim                       // the line drawn across the top of the input box
	Subtle      Style = Dim                       // text held one small step back from the subject
	Read        Style = hex(lime)                 // reading is on offer, and waiting to be granted
	Write       Style = hex(gold)                 // writing is on offer, and waiting to be granted
	Exec        Style = hex(red)                  // running a command is on offer, if you grant it
	Shell       Style = hex(steel)                // a shell prompt, tinted to match a command name
	Skill       Style = hex(mauve)                // a skill being read ahead of the work it guides
	History     Style = hex(mauve)                // rewriting the repository's history is on offer
	Pending     Style = col.Underline             // waiting for the keypress that follows a prefix
	Scrolled    Style = Dim                       // how much of the input is scrolled out of sight
	Chosen      Style = hex(copper)               // the row the cursor is resting on within a list
	Typed       Style = Normal                    // what the user typed when a session is replayed
	User        Style = background("#343541")     // a submitted message, kept apart from the reply
)

// Web marks permission to reach the internet through the web tools.
var Web Style = hex(steel)

// Network marks the name of a call that leaves the machine, which no other call does.
var Network Style = hex(red)

// The markdown of an answer.
var (
	Heading Style = hex(gold)   // a heading, which is drawn in bold as well
	Link    Style = hex(steel)  // what a link says
	Address Style = Dim         // where it goes
	Code    Style = hex(copper) // code within a line
	Block   Style = Dim         // code on lines of its own, where nothing highlights it
	Quote   Style = Dim         // what is quoted
	Bullet  Style = hex(copper) // what a list item is marked with
	Border  Style = Dim         // a rule, a table's borders, and the bar down a quote
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

func (self Style) Join(parts ...string) string {
	joined := util.JoinNonEmpty(parts...)
	if joined == "" {
		return ""
	}

	return self(joined)
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
