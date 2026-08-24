package migrate

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"crdx.org/duckopt/v2"

	"crdx.org/io/cmd/oh/config"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/format"
	"crdx.org/io/internal/xdg"
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

const maxLine = 64 * 1024 * 1024

type inputOpts struct {
	Migrate  bool     `docopt:"migrate"`
	DryRun   bool     `docopt:"--dry-run"`
	Sessions []string `docopt:"<session>"`
}

// Run migrates each named session, or every session written in an older format.
func Run() error {
	inputArgs := duckopt.MustBind[inputOpts](usage, "$0")

	path := configPath()
	configFrom, hasConfig, err := MigrateConfig(ConfigOptions{Path: path, DryRun: inputArgs.DryRun})
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if hasConfig && configFrom < config.Format {
		if !inputArgs.DryRun {
			fmt.Println(style.Subtle("copy kept in ") + configBackupPath(path))
		}
		fmt.Printf(
			"%s config %s\n",
			style.Subtle(verb(inputArgs.DryRun)),
			style.Subtle(fmt.Sprintf("%d → %d", configFrom, config.Format)),
		)
	}

	directory := sessionsDir()

	names := inputArgs.Sessions
	if len(names) == 0 {
		var err error
		if names, err = session.Outdated(directory); err != nil {
			return err
		}

		if len(names) == 0 {
			return sayNothingToDo(directory)
		}
	}

	options := Options{
		Directory: directory,
		BackupDir: backupDir(directory),
		DryRun:    inputArgs.DryRun,
	}

	if !options.DryRun {
		fmt.Println(style.Subtle("copies kept in ") + options.BackupDir)
	}

	failures := 0
	migrated := 0

	for _, name := range names {
		from, err := Session(options, name)
		if err != nil {
			failures++
			fmt.Fprintln(os.Stderr, style.Failure(name+": "+err.Error()))

			continue
		}

		if from == session.Format {
			fmt.Println(style.Subtle("already current ") + name)
			continue
		}

		migrated++

		fmt.Printf(
			"%s %s %s\n",
			style.Subtle(verb(options.DryRun)),
			name,
			style.Subtle(fmt.Sprintf("%d → %d", from, session.Format)),
		)
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d could not be migrated", failures, len(names))
	}

	fmt.Println(style.Subtle(fmt.Sprintf("%d of %d %s", migrated, len(names), summaryVerb(options.DryRun))))

	return nil
}

func sayNothingToDo(directory string) error {
	entries, err := session.Entries(directory)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println(style.Subtle("there are no stored sessions in ") + directory)
		return nil
	}

	fmt.Println(style.Subtle(fmt.Sprintf("all %d stored sessions are at format %d", len(entries), session.Format)))

	return nil
}

func verb(dryRun bool) string {
	if dryRun {
		return "would migrate"
	}

	return "migrated"
}

func summaryVerb(dryRun bool) string {
	if dryRun {
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

	lines, fromFormat, err := readJournal(journalPath)
	if err != nil {
		return 0, err
	}

	if err := format.Check(fromFormat, session.Format); err != nil {
		return fromFormat, fmt.Errorf("%w: upgrade oh", err)
	}

	if fromFormat == session.Format {
		return fromFormat, nil
	}

	appliedSteps := make([]step, 0, session.Format-fromFormat)
	for format := fromFormat; format < session.Format; format++ {
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

	lines[0]["version"] = json.RawMessage(fmt.Sprint(session.Format))

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
	return fmt.Sprintf("%s_pre_v%d", directory, session.Format)
}

func readJournal(path string) ([]map[string]json.RawMessage, int, error) {
	file, err := os.Open(path) //nolint:gosec // a journal named by a validated session name
	if err != nil {
		return nil, 0, err
	}

	defer func() { _ = file.Close() }()

	var lines []map[string]json.RawMessage

	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, maxLine)

	for scanner.Scan() {
		var line map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			return nil, 0, fmt.Errorf("line %d could not be read: %w", len(lines)+1, err)
		}

		lines = append(lines, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, err
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
		encoded, err := canonical(line)
		if err != nil {
			return fmt.Errorf("line %d: %w", index+1, err)
		}

		if _, err := writer.Write(append(encoded, '\n')); err != nil {
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
	assembled, err := json.Marshal(line)
	if err != nil {
		return nil, err
	}

	var record session.Line
	if err := json.Unmarshal(assembled, &record); err != nil {
		return nil, err
	}

	return json.Marshal(record)
}

func configPath() string {
	return xdg.ConfigPath("org.crdx", "oh", "config.toml")
}

func sessionsDir() string {
	return xdg.StatePath("org.crdx", "oh", "sessions")
}
