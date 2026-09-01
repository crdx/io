package restore

import (
	"fmt"

	"crdx.org/duckopt/v2"

	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/ohctl/console"
	"crdx.org/io/session"
)

const usage = `ohctl restore — restore archived sessions

Usage:
    $0 restore <session>...

An archived session is unpacked back into the sessions directory, where oh can list and resume it
again. List what has been archived with: ohctl sessions --archived
`

type inputOpts struct {
	Restore  bool     `docopt:"restore"`
	Sessions []string `docopt:"<session>"`
}

func Run() error {
	options := duckopt.MustBind[inputOpts](usage, "$0")
	return run(location.GetSessionsDir(), options.Sessions, console.Standard())
}

func run(directory string, names []string, output console.Output) error {
	failures := 0
	for _, name := range names {
		if err := session.Restore(directory, name); err != nil {
			failures++
			_, _ = fmt.Fprintln(output.Failure, style.Failure(name+": "+err.Error()))
			continue
		}
		_, _ = fmt.Fprintln(output.Screen, style.Subtle("restored ")+name)
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d could not be restored", failures, len(names))
	}

	return nil
}
