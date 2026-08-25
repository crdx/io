package sessions

import (
	"fmt"

	"crdx.org/io/session"
)

func ValidateFormats(directory string) error {
	ahead, err := session.Ahead(directory)
	if err != nil {
		return err
	}

	if len(ahead) > 0 {
		subject, _ := nameSessions(ahead)
		return fmt.Errorf(
			"%s written in a newer journal format than this oh reads (format %d): upgrade oh",
			subject, session.Format,
		)
	}

	outdated, err := session.Outdated(directory)
	if err != nil {
		return err
	}

	if len(outdated) == 0 {
		return nil
	}

	subject, object := nameSessions(outdated)

	return fmt.Errorf(
		"%s written in an older journal format: run `ohctl migrate` to bring %s up to format %d",
		subject, object, session.Format,
	)
}

func nameSessions(names []string) (string, string) {
	if len(names) == 1 {
		return names[0] + " is", "it"
	}

	return fmt.Sprintf("%d stored sessions are", len(names)), "them"
}
