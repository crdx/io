package main

import "testing"

func TestVersionIsNotEmpty(t *testing.T) {
	if version() == "" {
		t.Error("expected a version, got nothing")
	}
}
