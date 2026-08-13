package main

import (
	"fmt"
	"os"

	"crdx.org/io/codex"
)

func main() {
	if err := codex.Login(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println("Stored credentials in " + codex.CredentialsPath())
}
