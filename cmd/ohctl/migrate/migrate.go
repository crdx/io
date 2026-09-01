package migrate

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"crdx.org/duckopt/v2"

	"crdx.org/io/cmd/oh/config"
	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/ohctl/console"
	"crdx.org/io/internal/format"
	"crdx.org/io/session"
)

const usage = `ohctl migrate — bring configuration and stored sessions up to their current formats

Usage:
    $0 migrate [options] [<session>...]

Options:
    -n, --dry-run    Say what would be migrated without writing anything

The configuration is always considered. Sessions are named on the command line, or every outdated
stored session is done when none is.

The configuration file and each session bundle are copied aside before anything is written. A
session transcript is written again from the journal it was migrated into.
`

type inputOpts struct {
	Migrate  bool     `docopt:"migrate"`
	DryRun   bool     `docopt:"--dry-run"`
	Sessions []string `docopt:"<session>"`
}

// Run migrates each named session, or every session written in an older format.
func Run() error {
	return run(duckopt.MustBind[inputOpts](usage, "$0"), console.Standard())
}

func run(inputArgs *inputOpts, output console.Output) error {
	path := location.GetConfigFile()
	configFrom, hasConfig, err := MigrateConfig(ConfigOptions{Path: path, DryRun: inputArgs.DryRun})
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if hasConfig && configFrom < config.Format {
		if !inputArgs.DryRun {
			_, _ = fmt.Fprintln(output.Screen, style.Subtle("copy kept in ")+configBackupPath(path))
		}
		_, _ = fmt.Fprintf(
			output.Screen,
			"%s config %s\n",
			style.Subtle(verb(inputArgs.DryRun)),
			style.Subtle(fmt.Sprintf("%d → %d", configFrom, config.Format)),
		)
	}

	directory := location.GetSessionsDir()

	if err := sweepListingMeta(directory, inputArgs.DryRun, output); err != nil {
		return err
	}
	if err := sweepFastModes(directory, inputArgs.Sessions, inputArgs.DryRun, output); err != nil {
		return err
	}

	names := inputArgs.Sessions
	if len(names) == 0 {
		var err error
		if names, err = session.Outdated(directory); err != nil {
			return err
		}

		if len(names) == 0 {
			return sayNothingToDo(directory, output)
		}
	}

	options := Options{
		Directory: directory,
		BackupDir: backupDir(directory),
		DryRun:    inputArgs.DryRun,
	}

	if !options.DryRun {
		_, _ = fmt.Fprintln(output.Screen, style.Subtle("copies kept in ")+options.BackupDir)
	}

	failures := 0
	migratedCount := 0

	for _, name := range names {
		from, err := Session(options, name)
		if errors.Is(err, session.ErrInUse) {
			_, _ = fmt.Fprintf(output.Screen, "%s %s %s\n", style.Subtle("skipped"), name, style.Subtle("in use"))
			continue
		}
		if err != nil {
			failures++
			_, _ = fmt.Fprintln(output.Failure, style.Failure(name+": "+err.Error()))

			continue
		}

		if from == session.JournalFormat {
			_, _ = fmt.Fprintln(output.Screen, style.Subtle("already current ")+name)
			continue
		}

		migratedCount++

		_, _ = fmt.Fprintf(
			output.Screen,
			"%s %s %s\n",
			style.Subtle(verb(options.DryRun)),
			name,
			style.Subtle(fmt.Sprintf("%d → %d", from, session.JournalFormat)),
		)
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d could not be migrated", failures, len(names))
	}

	_, _ = fmt.Fprintln(output.Screen, style.Subtle(fmt.Sprintf("%d of %d %s", migratedCount, len(names), summaryVerb(options.DryRun))))

	return nil
}

func sweepFastModes(directory string, requestedSessionNames []string, isDryRun bool, output console.Output) error {
	names, err := fastModeNames(directory, requestedSessionNames)
	if err != nil {
		return err
	}

	options := Options{
		Directory: directory,
		BackupDir: directory + "_pre_fast_mode",
		DryRun:    isDryRun,
	}
	backfilledCount := 0
	failureCount := 0
	for _, name := range names {
		wasBackfilled, err := backfillFastMode(options, name)
		if errors.Is(err, session.ErrInUse) {
			continue
		}
		if err != nil {
			failureCount++
			_, _ = fmt.Fprintln(output.Failure, style.Failure(name+": "+err.Error()))
			continue
		}
		if wasBackfilled {
			backfilledCount++
		}
	}

	if failureCount > 0 {
		return fmt.Errorf("%d of %d could not have fast mode backfilled", failureCount, len(names))
	}
	if backfilledCount == 0 {
		return nil
	}
	if !isDryRun {
		_, _ = fmt.Fprintln(output.Screen, style.Subtle("copies kept in ")+options.BackupDir)
	}
	action := "backfilled"
	if isDryRun {
		action = "would backfill"
	}
	_, _ = fmt.Fprintf(output.Screen, "%s fast mode in %d\n", style.Subtle(action), backfilledCount)

	return nil
}

func sweepListingMeta(directory string, isDryRun bool, output console.Output) error {
	if isDryRun {
		stale, err := store.StaleMeta(directory)
		if err != nil {
			return err
		}
		if len(stale) > 0 {
			_, _ = fmt.Fprintf(output.Screen, "%s listing metadata of %d\n", style.Subtle("would rebuild"), len(stale))
		}
		return nil
	}

	rebuilt, err := store.RebuildStaleMeta(directory)
	if err != nil {
		return err
	}
	if rebuilt > 0 {
		_, _ = fmt.Fprintf(output.Screen, "%s listing metadata of %d\n", style.Subtle("rebuilt"), rebuilt)
	}

	return nil
}

func sayNothingToDo(directory string, output console.Output) error {
	entries, err := session.Entries(directory)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		_, _ = fmt.Fprintln(output.Screen, style.Subtle("there are no stored sessions in ")+directory)
		return nil
	}

	_, _ = fmt.Fprintln(output.Screen, style.Subtle(fmt.Sprintf("all %d stored sessions are at format %d", len(entries), session.JournalFormat)))

	return nil
}

func verb(isDryRun bool) string {
	if isDryRun {
		return "would migrate"
	}

	return "migrated"
}

func summaryVerb(isDryRun bool) string {
	if isDryRun {
		return "would be migrated"
	}

	return "migrated"
}

type Options struct {
	Directory string
	BackupDir string
	DryRun    bool
}

func Session(options Options, name string) (int, error) {
	directory := options.Directory
	journalPath := filepath.Join(directory, name, "session.jsonl")

	if !options.DryRun {
		heldLock, err := session.AcquireLock(directory, name)
		if err != nil {
			return 0, err
		}
		defer func() { _ = heldLock.Release() }()
	}

	lines, fromFormat, err := readJournal(journalPath)
	if err != nil {
		return 0, err
	}

	if err := format.Check(fromFormat, session.JournalFormat); err != nil {
		return fromFormat, fmt.Errorf("%w: upgrade oh", err)
	}

	if fromFormat == session.JournalFormat {
		return fromFormat, nil
	}

	appliedSteps := make([]step, 0, session.JournalFormat-fromFormat)
	for format := fromFormat; format < session.JournalFormat; format++ {
		migrationStep, ok := steps[format]
		if !ok {
			return fromFormat, fmt.Errorf("nothing knows how to migrate format %d", format)
		}
		appliedSteps = append(appliedSteps, migrationStep)

		if migrationStep.migrateLine != nil {
			for index, line := range lines {
				if err := migrationStep.migrateLine(line); err != nil {
					return fromFormat, fmt.Errorf("line %d: format %d: %w", index+1, format, err)
				}
			}
		}
		if migrationStep.migrateJournal != nil {
			lines, err = migrationStep.migrateJournal(lines)
			if err != nil {
				return fromFormat, fmt.Errorf("format %d: %w", format, err)
			}
		}
	}

	lines[0]["version"] = json.RawMessage(strconv.Itoa(session.JournalFormat))

	if options.DryRun {
		return fromFormat, nil
	}

	if err := keepCopy(filepath.Join(directory, name), filepath.Join(options.BackupDir, name)); err != nil {
		return fromFormat, err
	}

	if err := writeJournal(journalPath, lines); err != nil {
		return fromFormat, err
	}

	for index, migrationStep := range appliedSteps {
		if migrationStep.finalise == nil {
			continue
		}
		if err := migrationStep.finalise(directory, name); err != nil {
			return fromFormat, fmt.Errorf("the journal was migrated but format %d could not be finalised: %w", fromFormat+index, err)
		}
	}

	if err := store.Rebuild(directory, name); err != nil {
		return fromFormat, fmt.Errorf("the journal was migrated but its transcript was not: %w", err)
	}

	return fromFormat, nil
}

func keepCopy(bundlePath string, copyPath string) error {
	if _, err := os.Stat(copyPath); err == nil {
		return fmt.Errorf("a copy is already kept in %s: move it aside first", copyPath)
	}

	if err := os.MkdirAll(filepath.Dir(copyPath), 0o700); err != nil {
		return err
	}

	return os.CopyFS(copyPath, os.DirFS(bundlePath))
}

func backupDir(directory string) string {
	return fmt.Sprintf("%s_pre_v%d", directory, session.JournalFormat)
}

func readJournal(path string) ([]map[string]json.RawMessage, int, error) {
	file, err := os.Open(path) //nolint:gosec // a journal named by a validated session name
	if err != nil {
		return nil, 0, err
	}

	defer func() { _ = file.Close() }()

	var lines []map[string]json.RawMessage
	decoder := json.NewDecoder(bufio.NewReaderSize(file, 8192))

	for {
		var line map[string]json.RawMessage
		err := decoder.Decode(&line)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("line %d could not be read: %w", len(lines)+1, err)
		}

		lines = append(lines, line)
	}

	if len(lines) == 0 {
		return nil, 0, errors.New("the journal is empty")
	}

	return lines, formatOf(lines[0]), nil
}

func formatOf(head map[string]json.RawMessage) int {
	raw, ok := head["version"]
	if !ok {
		return 1
	}

	var format int
	if err := json.Unmarshal(raw, &format); err != nil || format == 0 {
		return 1
	}

	return format
}

func writeJournal(path string, lines []map[string]json.RawMessage) error {
	file, err := os.CreateTemp(filepath.Dir(path), "session-*.jsonl")
	if err != nil {
		return err
	}

	temporaryPath := file.Name()

	defer func() {
		_ = file.Close()
		_ = os.Remove(temporaryPath)
	}()

	writer := bufio.NewWriter(file)

	for index, line := range lines {
		encodedLine, err := canonical(line)
		if err != nil {
			return fmt.Errorf("line %d: %w", index+1, err)
		}

		if _, err := writer.Write(append(encodedLine, '\n')); err != nil {
			return err
		}
	}

	if err := writer.Flush(); err != nil {
		return err
	}

	if err := file.Sync(); err != nil {
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return err
	}

	return os.Rename(temporaryPath, path)
}

func canonical(line map[string]json.RawMessage) ([]byte, error) {
	assembledLine, err := json.Marshal(line)
	if err != nil {
		return nil, err
	}

	var record session.Line
	if err := json.Unmarshal(assembledLine, &record); err != nil {
		return nil, err
	}

	return json.Marshal(record)
}
