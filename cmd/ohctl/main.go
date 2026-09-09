package main

import (
	"fmt"
	"os"

	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/ohctl/analyse"
	"crdx.org/io/cmd/ohctl/complete"
	"crdx.org/io/cmd/ohctl/gc"
	"crdx.org/io/cmd/ohctl/migrate"
	"crdx.org/io/cmd/ohctl/regenerate"
	"crdx.org/io/cmd/ohctl/sessions"
)

const usage = `ohctl — oh control

Usage:
    ohctl sessions [options] [<filter>]
    ohctl analyse [options] [<session>...]
    ohctl regenerate [<session>...]
    ohctl migrate [options] [<session>...]
    ohctl gc [options]

Commands:
    sessions      List the stored sessions
    analyse       Analyse stored sessions
    regenerate    Write stored transcripts again from their journals
    migrate       Bring configuration and stored sessions up to their current formats
    gc            Remove the caches sessions leave behind
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
	case "regenerate":
		err = regenerate.Run()
	case "migrate":
		err = migrate.Run()
	case "gc":
		err = gc.Run()
	default:
		fmt.Print(usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
