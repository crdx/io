package agent

import (
	"iter"
	"strings"
)

// Coalesce combines runs of text fragments arrived via streaming into one coherent conversation.
func Coalesce(events iter.Seq2[Event, error]) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		var held strings.Builder

		release := func() bool {
			if held.Len() == 0 {
				return true
			}

			text := held.String()
			held.Reset()

			return yield(Event{Kind: Text, Text: text}, nil)
		}

		for event, err := range events {
			if err != nil {
				if release() {
					yield(Event{}, err)
				}

				return
			}

			if event.Kind == Text {
				held.WriteString(event.Text)
				continue
			}

			if !release() || !yield(event, nil) {
				return
			}
		}

		release()
	}
}
