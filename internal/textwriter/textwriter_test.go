package textwriter

import "testing"

func TestWhitespaceBetweenFragmentsCollapsesToOneSpace(t *testing.T) {
	var self Writer
	self.Text("  one   two ")
	self.Text("   three")

	if got := self.String(); got != "one two three" {
		t.Errorf("got %q", got)
	}
}

func TestAHeldSpaceIsDroppedWhenNothingFollowsIt(t *testing.T) {
	var self Writer
	self.Text("one")
	self.Text("   ")
	self.Newlines(1)

	if got := self.String(); got != "one\n" {
		t.Errorf("got %q", got)
	}
}

func TestRawMarkupJoinsWhatCameBeforeIt(t *testing.T) {
	var self Writer
	self.Text("one ")
	self.Raw("**")
	self.Text("two")
	self.Raw("**")

	if got := self.String(); got != "one **two**" {
		t.Errorf("got %q", got)
	}
}

func TestNewlinesCountTheOnesAlreadyWritten(t *testing.T) {
	var self Writer
	self.Text("one")
	self.Newlines(2)
	self.Newlines(2)
	self.Text("two")

	if got := self.String(); got != "one\n\ntwo" {
		t.Errorf("got %q", got)
	}
}

func TestNothingIsWrittenBeforeTheFirstFragment(t *testing.T) {
	var self Writer
	self.Text("   ")
	self.Text(" one")
	self.Raw("")

	if got := self.String(); got != "one" {
		t.Errorf("got %q", got)
	}
}
