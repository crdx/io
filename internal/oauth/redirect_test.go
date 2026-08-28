package oauth

import (
	"strings"
	"testing"
)

func TestCodeFromRedirectValidatesTheCompleteRedirect(t *testing.T) {
	code, err := CodeFromRedirect("  http://localhost:1455/callback?code=accepted&state=expected  \n", "expected")
	if err != nil {
		t.Fatal(err)
	}
	if code != "accepted" {
		t.Errorf("got code %q", code)
	}
}

func TestCodeFromRedirectRejectsInvalidRedirects(t *testing.T) {
	tests := map[string]struct {
		redirect string
		want     string
	}{
		"not a URL":        {redirect: "callback?code=one&state=expected", want: "complete URL"},
		"wrong state":      {redirect: "http://localhost/callback?code=one&state=wrong", want: "state did not match"},
		"missing code":     {redirect: "http://localhost/callback?state=expected", want: "no code"},
		"provider failure": {redirect: "http://localhost/callback?error=access_denied&state=expected", want: "access_denied"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := CodeFromRedirect(test.redirect, "expected")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("got %v", err)
			}
		})
	}
}
