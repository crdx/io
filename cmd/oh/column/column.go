package column

import (
	"strings"

	"crdx.org/io/cmd/oh/width"
)

const gap = 2

func Rows(values []string, cells int) []string {
	if len(values) == 0 {
		return nil
	}

	columnWidth := 0
	for _, value := range values {
		columnWidth = max(columnWidth, width.Of(value)+gap)
	}

	var rows []string
	var row strings.Builder
	used := 0
	for _, value := range values {
		if used > 0 && used+columnWidth > cells {
			rows = append(rows, strings.TrimRight(row.String(), " "))
			row.Reset()
			used = 0
		}
		row.WriteString(value)
		row.WriteString(strings.Repeat(" ", columnWidth-width.Of(value)))
		used += columnWidth
	}

	return append(rows, strings.TrimRight(row.String(), " "))
}
