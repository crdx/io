package sessions

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"crdx.org/duckopt/v2"

	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/oh/sessions/picker"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/cmd/ohctl/console"
	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/session"

	ohSessions "crdx.org/io/cmd/oh/sessions"
)

const usage = `ohctl sessions — list the stored sessions

Usage:
    $0 sessions [options]

Options:
    -j, --json               Write the listing as JSON
    -w, --workspace <dir>    List only the sessions of one workspace
    -r, --running            List only the sessions that are running
    -h, --help               Show this help
`

const (
	runningStatus = "running"
	endedStatus   = "ended"
	columnGap     = 2
	titleColumn   = 40
)

type inputOpts struct {
	Sessions  bool   `docopt:"sessions"`
	JSON      bool   `docopt:"--json"`
	Workspace string `docopt:"--workspace"`
	Running   bool   `docopt:"--running"`
}

// Listing is one stored session as the command reports it.
type Listing struct {
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	IsRunning    bool      `json:"isRunning"`
	Title        string    `json:"title"`
	WorkspaceDir string    `json:"workspaceDir"`
	ScratchDir   string    `json:"scratchDir"`
	SessionDir   string    `json:"sessionDir"`
	Model        string    `json:"model"`
	Effort       string    `json:"effort"`
	Messages     int       `json:"messages"`
	Started      time.Time `json:"started"`
	Touched      time.Time `json:"touched"`
}

// Run lists the stored sessions, newest first.
func Run() error {
	return run(duckopt.MustBind[inputOpts](usage, "$0"), console.Standard())
}

func run(options *inputOpts, output console.Output) error {
	directory := location.GetSessionsDir()
	if err := ohSessions.RefreshListings(directory, output.Failure); err != nil {
		return err
	}

	stored, err := ohSessions.Load(directory)
	if err != nil {
		if migrationError := ohSessions.ValidateFormats(directory); migrationError != nil {
			return migrationError
		}
		return err
	}

	if options.Workspace != "" {
		workspaceDir, err := filepath.Abs(options.Workspace)
		if err != nil {
			return fmt.Errorf("could not resolve the workspace path: %w", err)
		}
		stored = ohSessions.InWorkspace(stored, workspaceDir)
	}

	listings := describe(directory, stored, options.Running)
	if len(listings) == 0 {
		_, _ = fmt.Fprintln(output.Failure, style.Subtle("there are no stored sessions to list"))
	}

	if options.JSON {
		return writeJSON(listings, output.Screen)
	}

	return writeTable(listings, output.Screen)
}

func describe(directory string, stored []*picker.Session, runningOnly bool) []Listing {
	listings := make([]Listing, 0, len(stored))
	for _, storedSession := range stored {
		if runningOnly && !storedSession.IsRunning {
			continue
		}

		listings = append(listings, Listing{
			Name:         storedSession.Name,
			Status:       status(storedSession.IsRunning),
			IsRunning:    storedSession.IsRunning,
			Title:        oneLine(storedSession.Title),
			WorkspaceDir: storedSession.WorkspaceDir,
			ScratchDir:   location.GetTmpDir(storedSession.Name),
			SessionDir:   session.Dir(directory, storedSession.Name),
			Model:        storedSession.ModelID,
			Effort:       storedSession.Effort,
			Messages:     storedSession.MessageCount,
			Started:      storedSession.Started,
			Touched:      storedSession.Touched,
		})
	}

	return listings
}

func status(isRunning bool) string {
	if isRunning {
		return runningStatus
	}

	return endedStatus
}

func writeJSON(listings []Listing, writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "    ")
	return encoder.Encode(listings)
}

type tableRow struct {
	cells     []string
	isRunning bool
}

func writeTable(listings []Listing, writer io.Writer) error {
	if len(listings) == 0 {
		return nil
	}

	header := tableRow{cells: []string{
		"Status", "Agent", "Title", "Messages", "Length", "Last Message", "Model", "Effort", "Workspace",
	}}

	rows := []tableRow{header}
	for _, listing := range listings {
		rows = append(rows, tableRow{
			isRunning: listing.IsRunning,
			cells: []string{
				listing.Status,
				listing.Name,
				width.Elide(strutil.OrDash(listing.Title), titleColumn),
				strconv.Itoa(listing.Messages),
				util.CoarseDuration(listing.Touched.Sub(listing.Started)),
				util.Ago(listing.Touched),
				strutil.OrDash(listing.Model),
				strutil.OrDash(listing.Effort),
				listing.WorkspaceDir,
			},
		})
	}

	widths := columnWidths(rows)
	for index, row := range rows {
		line := joinColumns(row.cells, widths)

		switch {
		case index == 0:
			line = style.Subtle(line)
		case row.isRunning:
			line = style.Running(line)
		}

		if _, err := fmt.Fprintln(writer, line); err != nil {
			return err
		}
	}

	return nil
}

func columnWidths(rows []tableRow) []int {
	widths := make([]int, len(rows[0].cells))
	for _, row := range rows {
		for index, cell := range row.cells {
			widths[index] = max(widths[index], width.Of(cell))
		}
	}

	return widths
}

func joinColumns(cells []string, widths []int) string {
	var line strings.Builder
	for index, cell := range cells {
		line.WriteString(cell)

		if index < len(cells)-1 {
			padding := widths[index] - width.Of(cell) + columnGap
			line.WriteString(strings.Repeat(" ", padding))
		}
	}

	return line.String()
}

func oneLine(text string) string {
	return strings.ReplaceAll(text, "\n", " ")
}
