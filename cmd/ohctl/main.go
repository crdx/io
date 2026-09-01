package main

import (
	"fmt"
	"os"

	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/ohctl/analyse"
	"crdx.org/io/cmd/ohctl/complete"
	"crdx.org/io/cmd/ohctl/migrate"
	"crdx.org/io/cmd/ohctl/regen"
	"crdx.org/io/cmd/ohctl/restore"
	"crdx.org/io/cmd/ohctl/sessions"
)

const usage = `ohctl — oh control

Usage:
    ohctl sessions [options] [<filter>]
    ohctl restore <session>...
    ohctl analyse [options] [<session>...]
    ohctl regen [<session>...]
    ohctl migrate [options] [<session>...]

Commands:
    sessions    List the stored sessions
    restore     Restore archived sessions
    analyse     Analyse stored sessions
    regen       Write stored transcripts again from their journals
    migrate     Bring configuration and stored sessions up to their current formats
`

func main() {
	if complete.Write(os.Stdout, os.Args[1:]) {
		return
	}

	style.Init(os.Stdout)

	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "sessions":
		err = sessions.Run()
	case "analyse":
		err = analyse.Run()
	case "regen":
		err = regen.Run()
	case "restore":
		err = restore.Run()
	case "migrate":
		err = migrate.Run()
	default:
		fmt.Print(usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
