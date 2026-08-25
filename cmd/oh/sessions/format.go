package sessions

import (
	"fmt"

	"crdx.org/io/session"
)

func ValidateFormats(directory string) error {
	entries, err := session.Entries(directory)
	if err != nil {
		return err
	}
	var ahead, outdated []string
	for _, entry := range entries {
		if entry.Format > session.JournalFormat {
			ahead = append(ahead, entry.Name)
		} else if entry.Format < session.JournalFormat {
			outdated = append(outdated, entry.Name)
		}
	}

	if len(ahead) > 0 {
		subject, _ := nameSessions(ahead)
		return fmt.Errorf(
			"%s written in a newer journal format than this oh reads (format %d): upgrade oh",
			subject, session.JournalFormat,
		)
	}

	if len(outdated) == 0 {
		return nil
	}

	subject, object := nameSessions(outdated)

	return fmt.Errorf(
		"%s written in an older journal format: run `ohctl migrate` to bring %s up to format %d",
		subject, object, session.JournalFormat,
	)
}

func nameSessions(names []string) (string, string) {
	if len(names) == 1 {
		return names[0] + " is", "it"
	}

	return fmt.Sprintf("%d stored sessions are", len(names)), "them"
}
