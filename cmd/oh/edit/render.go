package edit

import (
	"crdx.org/io/cmd/oh/width"
)

const maxRows = 10

func window(rows []string, cursorRow int) Frame {
	if len(rows) <= maxRows {
		return Frame{Rows: rows, Row: cursorRow}
	}

	start := min(max(cursorRow-maxRows+1, 0), len(rows)-maxRows)

	return Frame{
		Rows:  rows[start : start+maxRows],
		Row:   cursorRow - start,
		Above: start,
		Below: len(rows) - start - maxRows,
	}
}

func layout(buffer *Buffer, room int) ([]string, int, int) {
	runes := buffer.Runes()
	laid := width.Rows(string(runes), room)

	rows := make([]string, 0, len(laid)+1)
	for _, row := range laid {
		rows = append(rows, row.Text)
	}

	if room > 0 && buffer.Cursor() == len(runes) && width.Of(rows[len(rows)-1]) >= room {
		return append(rows, ""), len(rows), 0
	}

	cursorRow, cursorColumn := locate(laid, runes, buffer.Cursor(), room)

	return rows, cursorRow, cursorColumn
}

func locate(rows []width.Row, runes []rune, cursor int, room int) (int, int) {
	for i, row := range rows {
		if cursor >= row.End && cursor <= row.Next && row.Next > row.End {
			return min(i+1, len(rows)-1), 0
		}

		if cursor >= row.Begin && cursor <= row.End {
			column := width.Of(string(runes[row.Begin:cursor]))
			if room > 0 {
				column = min(column, room)
			}

			return i, column
		}
	}

	return 0, 0
}
