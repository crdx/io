package main

import (
	"fmt"
	"os"

	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/ohctl/login"
	"crdx.org/io/cmd/ohctl/regen"
)

const usage = `ohctl — oh control

Usage:
    ohctl login [codex | opencode-go | anthropic]
    ohctl regen [<session>...]

Commands:
    login    Store provider credentials
    regen    Write stored transcripts again from their journals
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
		err = login.Run()
	case "regen":
		err = regen.Run()
	default:
		fmt.Print(usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
