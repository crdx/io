package sessionName

import (
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/session"
)

type state struct {
	name  string
	emoji string
}

func New(name string) segment.Factory {
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

		return state{name: name, emoji: emoji}, nil
	}
}

func (self state) Render(segment.Context) string {
	if self.emoji == "" {
		return style.Subtle(self.name)
	}

	return style.Subtle(self.name) + " " + self.emoji
}
