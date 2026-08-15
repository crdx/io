package agent

import (
	"iter"
	"strings"
)

// Coalesce combines runs of text fragments arrived via streaming into one coherent conversation.
func Coalesce(events iter.Seq2[Event, error]) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		var buf strings.Builder

		release := func() bool {
			if buf.Len() == 0 {
				return true
			}

			str := buf.String()
			buf.Reset()

			return yield(Event{Kind: Text, Payload: str}, nil)
		}

		for event, err := range events {
			if err != nil {
				if release() {
					yield(Event{}, err)
				}

				return
			}

			if event.Kind == Text {
				buf.WriteString(event.Payload)
				continue
			}

			if !release() || !yield(event, nil) {
				return
			}
		}

		release()
	}
}
