package model

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/table"
)

const (
	providerColumn   = 12
	recordedColumn   = 6
	selectableColumn = 10
	ignoredColumn    = 7
)

const nothingRecordedMark = "—"

type providerReport struct {
	Provider        string
	Source          string
	Why             string
	RecordedCount   int
	SelectableCount int
	IgnoredModels   []ignoredModel
}

func (self providerReport) hasRecorded() bool {
	return self.Why == ""
}

func updateTable() *table.Table {
	return table.New(
		table.Column{Title: "Provider", Width: providerColumn},
		table.Column{Title: "Models", Width: recordedColumn, Align: table.Right},
		table.Column{Title: "Selectable", Width: selectableColumn, Align: table.Right},
		table.Column{Title: "Ignored", Width: ignoredColumn, Align: table.Right},
		table.Column{Title: "Source"},
	)
}

func ignoredTable(rows [][]string) *table.Table {
	return table.New(
		table.Column{Title: "Provider"},
		table.Column{Title: "Model"},
		table.Column{Title: "Reason", Style: style.Subtle},
	).Fit(rows)
}

func writeLine(output io.Writer, line string) {
	_, _ = fmt.Fprintln(output, strings.TrimRight(line, " "))
}

func writeHeadings(output io.Writer) {
	writeLine(output, style.Column(updateTable().Header(0)))
}

func writeProviderReport(output io.Writer, report providerReport) {
	writeLine(output, updateTable().Row(providerCells(report), 0))
}

func ignoredRows(reports []providerReport) [][]string {
	var rows [][]string

	for _, report := range reports {
		for _, model := range report.IgnoredModels {
			rows = append(rows, []string{ProviderName(report.Provider), model.Name, model.Reason})
		}
	}

	return rows
}

func writeIgnoredModels(output io.Writer, reports []providerReport) {
	rows := ignoredRows(reports)
	if len(rows) == 0 {
		return
	}

	ignoredModelsTable := ignoredTable(rows)

	_, _ = fmt.Fprintln(output)
	writeLine(output, style.Column(ignoredModelsTable.Header(0)))

	for _, row := range rows {
		writeLine(output, ignoredModelsTable.Row(row, 0))
	}
}

func providerCells(report providerReport) []string {
	if !report.hasRecorded() {
		return []string{
			ProviderName(report.Provider),
			style.Subtle(nothingRecordedMark), "", "",
			style.Subtle(report.Why),
		}
	}

	return []string{
		ProviderName(report.Provider),
		strconv.Itoa(report.RecordedCount),
		selectableCount(report.SelectableCount),
		ignoredCount(len(report.IgnoredModels)),
		style.Information(report.Source),
	}
}

func selectableCount(count int) string {
	if count == 0 {
		return style.Subtle(count)
	}

	return style.Success(count)
}

func ignoredCount(count int) string {
	if count == 0 {
		return ""
	}

	return style.Change(count)
}

const ignoredHintCommand = "oh -u -I"

func writeIgnoredHint(output io.Writer, reports []providerReport) {
	var count int
	for _, report := range reports {
		count += len(report.IgnoredModels)
	}

	if count == 0 {
		return
	}

	_, _ = fmt.Fprintln(output, style.Subtle("Run ")+
		style.Accent(ignoredHintCommand)+
		style.Subtle(" to see "+ignoredSubject(count)+"."))
}

func ignoredSubject(count int) string {
	if count == 1 {
		return "the model that was ignored"
	}

	return fmt.Sprintf("the %d models that were ignored", count)
}
