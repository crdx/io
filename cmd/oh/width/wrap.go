package width

import (
	"strings"
	"unicode/utf8"

	"crdx.org/io/cmd/oh/escape"
)

const reset = "\x1b[0m"

// Row maps rendered text to source runes. Text may carry ANSI state across the break; [End, Next)
// is discarded break whitespace.
type Row struct {
	Text  string
	Begin int
	End   int
	Next  int
}

// Wrap breaks styled text into rows no wider than cells, preferring spaces. ANSI escapes take no
// cells, and their styling continues across breaks.
func Wrap(text string, cells int) []string {
	if cells <= 0 {
		return []string{text}
	}

	rows := Rows(text, cells)
	texts := make([]string, len(rows))

	for i, row := range rows {
		texts[i] = row.Text
	}

	return texts
}

// Rows wraps as Wrap does, and says which runes of the input each row came from. There is always at
// least one row.
func Rows(text string, cells int) []Row {
	var rows []Row

	start := 0

	for {
		line, rest, hasMore := strings.Cut(text, "\n")

		rows = append(rows, wrapLine(line, cells, start)...)

		if !hasMore {
			return rows
		}

		start += utf8.RuneCountInString(line) + 1
		text = rest
	}
}

type atom struct {
	text        string
	cells       int
	hyperlink   string
	isEscape    bool
	isStyle     bool
	isHyperlink bool
}

type presentationState struct {
	styles    string
	hyperlink string
}

func wrapLine(line string, cells int, base int) []Row {
	atoms := split(line)
	states := statesAt(atoms)
	offsets := offsetsOf(atoms)

	row := func(begin int, end int, next int) Row {
		return Row{
			Text:  join(atoms, begin, end, states),
			Begin: base + offsets[begin],
			End:   base + offsets[end],
			Next:  base + offsets[next],
		}
	}

	if cells <= 0 {
		return []Row{row(0, len(atoms), len(atoms))}
	}

	var rows []Row

	for begin := 0; begin < len(atoms); {
		end, space := reach(atoms, begin, cells)

		if end == len(atoms) {
			rows = append(rows, row(begin, end, end))
			break
		}

		if space > begin {
			from, after := run(atoms, space)

			if from == begin {
				from = end
			}

			rows = append(rows, row(begin, from, after))
			begin = after

			continue
		}

		rows = append(rows, row(begin, end, end))
		begin = end
	}

	if len(rows) == 0 {
		return []Row{row(0, 0, 0)}
	}

	return rows
}

func run(atoms []atom, at int) (int, int) {
	from, after := at, at

	for from > 0 && atoms[from-1].text == " " {
		from--
	}

	for after < len(atoms) && atoms[after].text == " " {
		after++
	}

	return from, after
}

func offsetsOf(atoms []atom) []int {
	out := make([]int, len(atoms)+1)

	for i, one := range atoms {
		out[i+1] = out[i] + len([]rune(one.text))
	}

	return out
}

func reach(atoms []atom, begin int, cells int) (int, int) {
	takenCells := 0
	space := -1
	end := begin

	for ; end < len(atoms); end++ {
		one := atoms[end]

		if one.isEscape && one.cells == 0 {
			continue
		}

		if takenCells+one.cells > cells {
			if one.text == " " {
				space = end
			}

			break
		}

		if one.text == " " {
			space = end
		}

		takenCells += one.cells
	}

	if end == begin {
		end = advance(atoms, begin)
	}

	return end, space
}

func advance(atoms []atom, begin int) int {
	end := begin

	for end < len(atoms) && atoms[end].isEscape && atoms[end].cells == 0 {
		end++
	}

	return min(end+1, len(atoms))
}

func join(atoms []atom, begin int, end int, states []presentationState) string {
	span := 0
	for _, one := range atoms[begin:end] {
		span += len(one.text)
	}

	var out strings.Builder
	out.Grow(len(states[begin].hyperlink) + len(states[begin].styles) + span + len(reset) + len(escape.HyperlinkClose))

	out.WriteString(states[begin].hyperlink)
	out.WriteString(states[begin].styles)

	for _, one := range atoms[begin:end] {
		out.WriteString(one.text)
	}

	if states[end].styles != "" {
		out.WriteString(reset)
	}
	if states[end].hyperlink != "" {
		out.WriteString(escape.HyperlinkClose)
	}

	return out.String()
}

func statesAt(atoms []atom) []presentationState {
	states := make([]presentationState, len(atoms)+1)

	for i, one := range atoms {
		states[i+1] = states[i]

		if one.isHyperlink {
			states[i+1].hyperlink = one.hyperlink
			continue
		}
		if !one.isStyle {
			continue
		}
		if one.text == reset || one.text == "\x1b[m" {
			states[i+1].styles = ""
			continue
		}
		states[i+1].styles += one.text
	}

	return states
}

func split(text string) []atom {
	atoms := make([]atom, 0, utf8.RuneCountInString(text))

	runes := []rune(text)

	for i := 0; i < len(runes); {
		if runes[i] == '\x1b' {
			sequence := escape.GetSequence(runes, i)
			atoms = append(atoms, atom{
				text:        string(runes[i:sequence.End]),
				cells:       sequence.Cells,
				hyperlink:   sequence.Hyperlink,
				isEscape:    true,
				isStyle:     sequence.IsStyle,
				isHyperlink: sequence.IsHyperlink,
			})
			i = sequence.End

			continue
		}

		end := i + 1
		for end < len(runes) && runes[end] != '\x1b' {
			end++
		}

		for grapheme, cells := range Graphemes(string(runes[i:end])) {
			atoms = append(atoms, atom{text: grapheme, cells: cells})
		}
		i = end
	}

	return atoms
}

// Ellipsis marks where text was cut.
const Ellipsis = "…"

// Elide cuts text to the cells it has, marking what it dropped with an ellipsis.
func Elide(text string, cells int) string {
	if cells <= 0 {
		return ""
	}

	if Of(text) <= cells {
		return text
	}

	if cells == 1 {
		return Ellipsis
	}

	keptText, _ := Cut(text, cells-1)

	return keptText + Ellipsis
}
