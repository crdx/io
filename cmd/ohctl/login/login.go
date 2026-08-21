package login

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"crdx.org/duckopt/v2"
	"crdx.org/io/provider/anthropic"
	"crdx.org/io/provider/chat"
	"crdx.org/io/provider/codex"
	"golang.org/x/term"
)

const usage = `ohctl login — store provider credentials

Usage:
    $0 login [codex | opencode-go | anthropic]

Providers:
    codex         Authorise a ChatGPT subscription with OAuth [default]
    opencode-go   Store an OpenCode Go API key
    anthropic     Authorise a Claude subscription with OAuth
`

type inputOpts struct {
	Login      bool `docopt:"login"`
	Codex      bool `docopt:"codex"`
	OpenCodeGo bool `docopt:"opencode-go"`
	Anthropic  bool `docopt:"anthropic"`
}

func Run() error {
	options := duckopt.MustBind[inputOpts](usage, "$0")

	var path string
	var err error
	switch {
	case options.OpenCodeGo:
		path = chat.CredentialsPath()
		err = loginOpenCodeGo(os.Stdin, os.Stdout, path)
	case options.Anthropic:
		path = anthropic.CredentialsPath()
		err = anthropic.Login()
	case options.Codex:
		path = codex.CredentialsPath()
		err = codex.Login()
	default:
		err = errors.New("tell me a provider")
	}

	if err != nil {
		return err
	}

	fmt.Println("Stored credentials in " + path)
	return nil
}

func loginOpenCodeGo(input io.Reader, output io.Writer, path string) error {
	key, err := readOpenCodeGoKey(input, output)
	if err != nil {
		return err
	}

	return chat.SaveKeyAt(path, key)
}

func readOpenCodeGoKey(input io.Reader, output io.Writer) (string, error) {
	if _, err := fmt.Fprint(output, "OpenCode Go API key: "); err != nil {
		return "", err
	}

	var key string
	if terminal, ok := input.(*os.File); ok && term.IsTerminal(int(terminal.Fd())) {
		data, err := term.ReadPassword(int(terminal.Fd()))
		if err != nil {
			return "", fmt.Errorf("read API key: %w", err)
		}
		key = string(data)
		if _, err := fmt.Fprintln(output); err != nil {
			return "", err
		}
	} else {
		line, err := bufio.NewReader(input).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read API key: %w", err)
		}
		key = line
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("OpenCode Go API key is empty")
	}

	return key, nil
}
