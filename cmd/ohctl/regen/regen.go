// Package regen writes stored transcripts again from the journals they were rendered from.
package regen

import (
	"fmt"
	"os"

	"crdx.org/duckopt/v2"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/xdgutil"
	"crdx.org/io/session"
)

const usage = `ohctl regen — write stored transcripts again

Usage:
    $0 regen [<session>...]

Sessions are named on the command line, or every stored session is done when none is.
`

type inputOpts struct {
	Regen    bool     `docopt:"regen"`
	Sessions []string `docopt:"<session>"`
}

// Run rewrites the transcript of each named session, or of every stored session.
func Run() error {
	options := duckopt.MustBind[inputOpts](usage, "$0")

	directory := sessionsDir()
	names := options.Sessions
	if len(names) == 0 {
		var err error
		if names, err = storedNames(directory); err != nil {
			return err
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("there are no stored sessions in %s", directory)
	}

	failures := 0
	for _, name := range names {
		if err := store.Rebuild(directory, name); err != nil {
			failures++
			fmt.Fprintln(os.Stderr, style.Failure(name+": "+err.Error()))
			continue
		}
		fmt.Println(style.Subtle("wrote ") + name)
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d could not be written", failures, len(names))
	}

	fmt.Println(style.Subtle(fmt.Sprintf("%d transcripts written", len(names))))
	return nil
}

func storedNames(directory string) ([]string, error) {
	entries, err := session.Entries(directory)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	return names, nil
}

func sessionsDir() string {
	return xdgutil.StatePath("org.crdx", "oh", "sessions")
}
