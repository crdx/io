package complete

import (
	"fmt"
	"io"
	"strings"

	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/session"
)

const Flag = "--complete"

const (
	kindCommand  = "command"
	kindSession  = "session"
	kindArchived = "archived"
)

var commands = []string{"analyse", "migrate", "regen", "restore", "sessions"}

func Write(out io.Writer, args []string) bool {
	kind, word, isWanted := request(args)
	if !isWanted {
		return false
	}

	for _, completion := range completions(kind, word) {
		_, _ = fmt.Fprintln(out, completion)
	}

	return true
}

func request(args []string) (string, string, bool) {
	if len(args) < 2 || args[0] != Flag {
		return "", "", false
	}

	var word string
	if len(args) > 2 {
		word = args[2]
	}

	return args[1], word, true
}

func completions(kind string, word string) []string {
	switch kind {
	case kindCommand:
		return withPrefix(word, commands)
	case kindSession:
		return withPrefix(word, storedNames())
	case kindArchived:
		return withPrefix(word, archivedNames())
	default:
		return nil
	}
}

func withPrefix(word string, candidates []string) []string {
	var matches []string

	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, word) {
			matches = append(matches, candidate)
		}
	}

	return matches
}

func storedNames() []string {
	entries, err := session.Entries(location.GetSessionsDir())
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}

	return names
}

func archivedNames() []string {
	names, err := session.ArchivedNames(location.GetSessionsDir())
	if err != nil {
		return nil
	}

	return names
}
