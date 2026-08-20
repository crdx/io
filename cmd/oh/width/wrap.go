package width

import "strings"

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

	for index, row := range rows {
		texts[index] = row.Text
	}

	return texts
}

// Rows wraps as Wrap does, and says which runes of the input each row came from. There is always at
// least one row.
func Rows(text string, cells int) []Row {
	runes := []rune(text)

	var rows []Row

	start := 0

	for index := 0; index <= len(runes); index++ {
		if index < len(runes) && runes[index] != '\n' {
			continue
		}

		rows = append(rows, wrapLine(string(runes[start:index]), cells, start)...)
		start = index + 1
	}

	return rows
}

type atom struct {
	text     string
	cells    int
	isEscape bool
}

func wrapLine(line string, cells int, base int) []Row {
	atoms := split(line)
	openStyles := stylesAt(atoms)
	offsets := offsetsOf(atoms)

	row := func(begin int, end int, next int) Row {
		return Row{
			Text:  join(atoms, begin, end, openStyles),
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
				from = end // whitespace with nothing before it is indentation, not a break
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

	for index, one := range atoms {
		out[index+1] = out[index] + len([]rune(one.text))
	}

	return out
}

func reach(atoms []atom, begin int, cells int) (int, int) {
	takenCells := 0
	space := -1
	end := begin

	for ; end < len(atoms); end++ {
		one := atoms[end]

		if one.isEscape {
			continue
		}

		if takenCells+one.cells > cells {
			if one.text == " " {
				space = end // dropping the space that overruns is what makes the row fit
			}

			break
		}

		if one.text == " " {
			space = end
		}

		takenCells += one.cells
	}

	if end == begin {
		end = advance(atoms, begin) // a character wider than the row still has to go somewhere
	}

	return end, space
}

func advance(atoms []atom, begin int) int {
	end := begin

	for end < len(atoms) && atoms[end].isEscape {
		end++
	}

	return min(end+1, len(atoms))
}

func join(atoms []atom, begin int, end int, openStyles []string) string {
	var out strings.Builder

	out.WriteString(openStyles[begin])

	for _, one := range atoms[begin:end] {
		out.WriteString(one.text)
	}

	if openStyles[end] != "" {
		out.WriteString(reset)
	}

	return out.String()
}

func stylesAt(atoms []atom) []string {
	openStyles := make([]string, len(atoms)+1)

	for index, one := range atoms {
		switch {
		case !one.isEscape:
			openStyles[index+1] = openStyles[index]
		case one.text == reset || one.text == "\x1b[m":
			openStyles[index+1] = ""
		default:
			openStyles[index+1] = openStyles[index] + one.text
		}
	}

	return openStyles
}

func split(text string) []atom {
	var atoms []atom

	runes := []rune(text)

	for index := 0; index < len(runes); {
		if runes[index] == '\x1b' {
			end := index + 1

			for end < len(runes) && runes[end] != 'm' && runes[end] != 'K' {
				end++
			}

			if end < len(runes) {
				end++
			}

			atoms = append(atoms, atom{text: string(runes[index:end]), isEscape: true})
			index = end

			continue
		}

		size := Rune(runes[index])

		if size == 0 && len(atoms) > 0 && !atoms[len(atoms)-1].isEscape {
			atoms[len(atoms)-1].text += string(runes[index])
			index++

			continue
		}

		atoms = append(atoms, atom{text: string(runes[index]), cells: size})
		index++
	}

	return atoms
}
