package key

import (
	"bufio"
	"strconv"
	"strings"
	"unicode"
)

// Code is which key was pressed.
type Code int

// The keys a decoder reports.
const (
	Rune Code = iota
	Enter
	Backspace
	Delete
	Left
	Right
	Up
	Down
	Home
	End
	PageUp
	PageDown
	Escape
	PasteStart
	PasteEnd
	FocusIn
	FocusOut
	Unknown
)

// Modifier is what was held down with the key.
type Modifier int

// The modifiers a key can carry.
const (
	Shift Modifier = 1 << iota
	Alt
	Ctrl
)

// Has says whether every modifier in the mask was held.
func (self Modifier) Has(mask Modifier) bool {
	return self&mask == mask
}

// Key is one keypress.
type Key struct {
	Code  Code
	Value rune
	Mod   Modifier
}

// Decoder reads keypresses off a terminal.
type Decoder struct {
	reader *bufio.Reader
}

// NewDecoder builds a decoder over a buffered terminal.
func NewDecoder(reader *bufio.Reader) *Decoder {
	return &Decoder{reader: reader}
}

const (
	escapeByte = '\x1b'
	delByte    = '\x7f'
)

// Enable turns the keyboard protocol, bracketed paste, and focus reporting on. Disable puts the
// terminal back.
const (
	Enable  = "\x1b[>1u\x1b[?2004h\x1b[?1004h"
	Disable = "\x1b[?1004l\x1b[?2004l\x1b[<u"
)

// Next blocks until a key is pressed.
func (self *Decoder) Next() (Key, error) {
	first, _, err := self.reader.ReadRune()
	if err != nil {
		return Key{}, err
	}

	if first == escapeByte {
		return self.sequence()
	}

	if first == '\r' {
		self.swallow('\n')

		return Key{Code: Enter}, nil
	}

	return plain(first), nil
}

func (self *Decoder) swallow(value rune) {
	if self.reader.Buffered() == 0 {
		return
	}

	if next, err := self.reader.Peek(1); err == nil && rune(next[0]) == value {
		_, _ = self.reader.Discard(1)
	}
}

func plain(value rune) Key {
	switch {
	case value == '\n':
		return Key{Code: Enter}
	case value == '\t':
		return Key{Code: Rune, Value: '\t'}
	case value == delByte:
		return Key{Code: Backspace}
	case value >= 1 && value <= 26:
		return Key{Code: Rune, Value: value + 'a' - 1, Mod: Ctrl}
	case value < ' ':
		return Key{Code: Unknown}
	}

	return Key{Code: Rune, Value: value}
}

func (self *Decoder) sequence() (Key, error) {
	if self.reader.Buffered() == 0 {
		return Key{Code: Escape}, nil
	}

	next, _, err := self.reader.ReadRune()
	if err != nil {
		return Key{}, err
	}

	switch next {
	case '[':
		return self.parameters()
	case 'O':
		return self.applicationCursor()
	}

	return Key{Code: Unknown}, nil
}

func (self *Decoder) applicationCursor() (Key, error) {
	final, _, err := self.reader.ReadRune()
	if err != nil {
		return Key{}, err
	}

	code, found := letters[final]
	if !found {
		return Key{Code: Unknown}, nil
	}

	return Key{Code: code}, nil
}

func (self *Decoder) parameters() (Key, error) {
	var parameters strings.Builder

	for {
		next, _, err := self.reader.ReadRune()
		if err != nil {
			return Key{}, err
		}

		if (next >= '0' && next <= '9') || next == ';' || next == ':' {
			parameters.WriteRune(next)
			continue
		}

		return csi(parameters.String(), next), nil
	}
}

func csi(parameters string, final rune) Key {
	switch final {
	case 'u':
		return unicodeKey(parameters)
	case '~':
		return tilde(parameters)
	case 'I':
		return Key{Code: FocusIn}
	case 'O':
		return Key{Code: FocusOut}
	}

	code, found := letters[final]
	if !found {
		return Key{Code: Unknown}
	}

	return Key{Code: code, Mod: modifiers(parameters)}
}

var letters = map[rune]Code{
	'A': Up,
	'B': Down,
	'C': Right,
	'D': Left,
	'F': End,
	'H': Home,
}

var codepoints = map[int]Code{
	13:  Enter,
	27:  Escape,
	127: Backspace,
}

func unicodeKey(parameters string) Key {
	number, found := field(parameters, 0)
	if !found {
		return Key{Code: Unknown}
	}

	modifier := modifiers(parameters)

	if code, found := codepoints[number]; found {
		return Key{Code: code, Mod: modifier}
	}

	if number < 0 || number > unicode.MaxRune {
		return Key{Code: Unknown}
	}

	return Key{Code: Rune, Value: rune(number), Mod: modifier}
}

func tilde(parameters string) Key {
	number, _ := field(parameters, 0)

	switch number {
	case 3:
		return Key{Code: Delete, Mod: modifiers(parameters)}
	case 5:
		return Key{Code: PageUp, Mod: modifiers(parameters)}
	case 6:
		return Key{Code: PageDown, Mod: modifiers(parameters)}
	case 200:
		return Key{Code: PasteStart}
	case 201:
		return Key{Code: PasteEnd}
	}

	return Key{Code: Unknown}
}

func modifiers(parameters string) Modifier {
	encodedModifier, found := field(parameters, 1)
	if !found || encodedModifier < 1 {
		return 0
	}

	return Modifier(encodedModifier - 1)
}

func field(parameters string, index int) (int, bool) {
	fields := strings.Split(parameters, ";")
	if index >= len(fields) {
		return 0, false
	}

	head, _, _ := strings.Cut(fields[index], ":")

	value, err := strconv.Atoi(head)
	if err != nil {
		return 0, false
	}

	return value, true
}
