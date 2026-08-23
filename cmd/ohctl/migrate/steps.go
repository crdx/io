package migrate

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type step func(line map[string]json.RawMessage) error

var steps = map[int]step{
	1: emphasisReplacesHighlight,
}

func emphasisReplacesHighlight(line map[string]json.RawMessage) error {
	return with(line, func(event map[string]json.RawMessage) error {
		highlight, ok := event["highlight"]
		if !ok {
			return nil
		}

		delete(event, "highlight")
		event["emphasis"] = highlight

		return nil
	})
}

func with(line map[string]json.RawMessage, visit func(map[string]json.RawMessage) error) error {
	raw, ok := line["event"]
	if !ok {
		return nil
	}

	var event map[string]json.RawMessage
	if err := json.Unmarshal(raw, &event); err != nil {
		return fmt.Errorf("the event could not be read: %w", err)
	}

	before, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if err := visit(event); err != nil {
		return err
	}

	after, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if !bytes.Equal(before, after) {
		line["event"] = after
	}

	return nil
}
