package main

import (
	"fmt"
	"os"

	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/ohctl/login"
)

const usage = `ohctl — oh control

Usage:
    ohctl login [codex | opencode-go | anthropic]

Commands:
    login    Store provider credentials
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
	default:
		fmt.Print(usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
