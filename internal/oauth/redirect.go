package oauth

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func CodeFromRedirect(written string, expectedState string) (string, error) {
	redirect, err := url.Parse(strings.TrimSpace(written))
	if err != nil || !redirect.IsAbs() || redirect.Host == "" {
		return "", errors.New("the pasted redirect is not a complete URL")
	}

	query := redirect.Query()
	if failure := query.Get("error"); failure != "" {
		return "", fmt.Errorf("authorisation failed: %s", failure)
	}
	if query.Get("state") != expectedState {
		return "", errors.New("the pasted redirect state did not match")
	}
	if code := query.Get("code"); code != "" {
		return code, nil
	}
	return "", errors.New("the pasted redirect carried no code")
}
