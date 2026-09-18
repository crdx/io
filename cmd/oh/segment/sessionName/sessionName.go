package sessionName

import (
	"net/url"
	"path/filepath"

	"crdx.org/io/cmd/oh/link"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/session"
)

type state struct {
	name        string
	emoji       string
	address     string
	isPersisted func() bool
}

func New(name string, directory string, isPersisted func() bool) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		var args struct {
			Emoji bool `toml:"emoji"`
		}

		if err := options.Read(&args); err != nil {
			return nil, err
		}

		emoji := ""
		if args.Emoji {
			emoji = session.Emoji(name)
		}

		address := ""
		if directory != "" {
			address = (&url.URL{Scheme: "file", Path: filepath.ToSlash(directory)}).String()
		}

		return state{name: name, emoji: emoji, address: address, isPersisted: isPersisted}, nil
	}
}

func (self state) Render(segment.Context) string {
	text := style.Subtle(self.name)
	if self.address != "" && self.isPersisted != nil && self.isPersisted() {
		text = link.RenderURL(text, self.address)
	}
	if self.emoji != "" {
		text += " " + self.emoji
	}

	return text
}
