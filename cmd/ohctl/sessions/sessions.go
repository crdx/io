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
	"crdx.org/io/cmd/oh/segment/fastMode"
	"crdx.org/io/cmd/oh/sessions/picker"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/cmd/oh/work"
	"crdx.org/io/cmd/ohctl/console"
	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/session"

	ohSessions "crdx.org/io/cmd/oh/sessions"
)

const usage = `ohctl sessions — list the stored sessions

Usage:
    $0 sessions [options] [<filter>]

Options:
    -j, --json               Write the listing as JSON
    -w, --workspace <dir>    List only the sessions of one workspace
    -r, --running            List only the sessions that are running
    -a, --archived           List the archived sessions rather than the stored ones
    -h, --help               Show this help

The newest 50 sessions are listed, unless a filter of more than three characters is given, which
lists every session it matches.
`

const (
	runningStatus  = "running"
	endedStatus    = "ended"
	archivedStatus = "archived"
	columnGap      = 2
	titleColumn    = 40
	listLimit      = 50
	shortFilter    = 3
)

type inputOpts struct {
	Sessions  bool   `docopt:"sessions"`
	JSON      bool   `docopt:"--json"`
	Workspace string `docopt:"--workspace"`
	Running   bool   `docopt:"--running"`
	Archived  bool   `docopt:"--archived"`
	Filter    string `docopt:"<filter>"`
}

type Listing struct {
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	IsRunning    bool      `json:"isRunning"`
	IsArchived   bool      `json:"isArchived"`
	IsFast       bool      `json:"isFast"`
	Title        string    `json:"title"`
	WorkspaceDir string    `json:"workspaceDir"`
	ScratchDir   string    `json:"scratchDir"`
	SessionDir   string    `json:"sessionDir"`
	Model        string    `json:"model"`
	Effort       string    `json:"effort"`
	Messages     int       `json:"messages"`
	StartedAt    time.Time `json:"started"`
	TouchedAt    time.Time `json:"touched"`
}

func Run() error {
	return run(duckopt.MustBind[inputOpts](usage, "$0"), console.Standard())
}

func run(options *inputOpts, output console.Output) error {
	directory := location.GetSessionsDir()
	if err := ohSessions.RefreshListings(directory, output.Failure); err != nil {
		return err
	}

	storedSessions, err := loadSessions(directory, options.Archived)
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
		storedSessions = ohSessions.InWorkspace(storedSessions, work.At(workspaceDir))
	}

	storedSessions = matching(storedSessions, options.Filter)

	listings := describe(directory, storedSessions, options.Running)
	if len(listings) == 0 {
		_, _ = fmt.Fprintln(output.Failure, style.Subtle(nothingToList(options)))
	}

	listings = withinLimit(listings, options.Filter, output.Failure)

	if options.JSON {
		return writeJSON(listings, output.Screen)
	}

	return writeTable(listings, output.Screen)
}

func nothingToList(options *inputOpts) string {
	kind := "stored"
	if options.Archived {
		kind = "archived"
	}
	if options.Filter != "" {
		return fmt.Sprintf("no %s session matches %q", kind, options.Filter)
	}

	return "there are no " + kind + " sessions to list"
}

func loadSessions(directory string, isArchivedWanted bool) ([]*picker.Session, error) {
	if isArchivedWanted {
		return ohSessions.LoadArchived(directory)
	}

	return ohSessions.Load(directory)
}

func matching(storedSessions []*picker.Session, filter string) []*picker.Session {
	if filter == "" {
		return storedSessions
	}

	matches := make([]*picker.Session, 0, len(storedSessions))
	for _, storedSession := range storedSessions {
		if strutil.MatchesQuery(storedSession.Text(), filter) {
			matches = append(matches, storedSession)
		}
	}

	return matches
}

func withinLimit(listings []Listing, filter string, failure io.Writer) []Listing {
	if len(filter) > shortFilter || len(listings) <= listLimit {
		return listings
	}

	_, _ = fmt.Fprintln(failure, style.Subtle(fmt.Sprintf(
		"listing the newest %d of %d sessions, which a filter of more than %d characters lists in full",
		listLimit, len(listings), shortFilter,
	)))

	return listings[:listLimit]
}

func describe(directory string, storedSessions []*picker.Session, isRunningOnly bool) []Listing {
	listings := make([]Listing, 0, len(storedSessions))
	for _, storedSession := range storedSessions {
		if isRunningOnly && !storedSession.IsRunning {
			continue
		}

		listings = append(listings, Listing{
			Name:         storedSession.Name,
			Status:       status(storedSession),
			IsRunning:    storedSession.IsRunning,
			IsArchived:   storedSession.IsArchived,
			IsFast:       storedSession.IsFast,
			Title:        oneLine(storedSession.Title),
			WorkspaceDir: storedSession.WorkspaceDir,
			ScratchDir:   location.GetTmpDir(storedSession.Name),
			SessionDir:   sessionPath(directory, storedSession),
			Model:        storedSession.ModelID,
			Effort:       storedSession.Effort,
			Messages:     storedSession.MessageCount,
			StartedAt:    storedSession.StartedAt,
			TouchedAt:    storedSession.TouchedAt,
		})
	}

	return listings
}

func status(storedSession *picker.Session) string {
	switch {
	case storedSession.IsArchived:
		return archivedStatus
	case storedSession.IsRunning:
		return runningStatus
	default:
		return endedStatus
	}
}

func sessionPath(directory string, storedSession *picker.Session) string {
	if storedSession.IsArchived {
		return session.ArchivePath(directory, storedSession.Name)
	}

	return session.Dir(directory, storedSession.Name)
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
				util.CoarseDuration(listing.TouchedAt.Sub(listing.StartedAt)),
				util.Ago(listing.TouchedAt),
				modelName(listing),
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

func modelName(listing Listing) string {
	name := strutil.OrDash(listing.Model)
	if listing.IsFast {
		return fastMode.GetMark(true) + " " + name
	}

	return name
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
