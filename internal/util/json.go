package util

import (
	"encoding/json"
	"strconv"
)

func JSONScalar(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}

	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}

	var isTrue bool
	if json.Unmarshal(raw, &isTrue) == nil {
		return strconv.FormatBool(isTrue)
	}

	return ""
}
