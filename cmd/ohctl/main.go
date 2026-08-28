package main

import (
	"context"
	"fmt"
	"os"

	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/ohctl/login"
	"crdx.org/io/cmd/ohctl/migrate"
	"crdx.org/io/cmd/ohctl/regen"
	"crdx.org/io/cmd/ohctl/sessions"
)

const usage = `ohctl — oh control

Usage:
    ohctl login [codex | opencode-go | anthropic]
    ohctl sessions [options]
    ohctl regen [<session>...]
    ohctl migrate [options] [<session>...]

Commands:
    login       Store provider credentials
    sessions    List the stored sessions
    regen       Write stored transcripts again from their journals
    migrate     Bring configuration and stored sessions up to their current formats
`

func main() {
	style.Init(os.Stdout)

	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "login":
		err = login.Run(context.Background())
	case "sessions":
		err = sessions.Run()
	case "regen":
		err = regen.Run()
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
