package line

import "testing"

func TestUnescapeKeepsMalformedBackslashEscapes(t *testing.T) {
	for encodedText, want := range map[string]string{
		`path\`:     `path\`,
		`path\file`: `path\file`,
	} {
		if got := unescape(encodedText); got != want {
			t.Errorf("unescape(%q) = %q, want %q", encodedText, got, want)
		}
	}
}
