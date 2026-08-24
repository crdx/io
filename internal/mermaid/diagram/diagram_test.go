package diagram

import (
	"reflect"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	if config.UseAscii || config.BoxBorderPadding != 1 || config.PaddingBetweenX != 5 || config.PaddingBetweenY != 5 || config.StyleType != "cli" || config.SequenceParticipantSpacing != 5 || config.SequenceMessageSpacing != 1 || config.SequenceSelfMessageWidth != 4 {
		t.Errorf("unexpected defaults: %#v", config)
	}
}

func TestStripFrontmatterCoversEveryDelimiterAndTitleForm(t *testing.T) {
	for name, test := range map[string]struct {
		input     string
		wantBody  string
		wantTitle string
	}{
		"none": {
			"graph LR\nA --> B",
			"graph LR\nA --> B",
			"",
		},
		"plain title": {
			"---\ntitle: Diagram\n---\ngraph LR",
			"graph LR",
			"Diagram",
		},
		"quoted title": {
			"---\ntitle: \"Diagram # one\"\n---\ngraph LR",
			"graph LR",
			"Diagram # one",
		},
		"unquoted title comment": {
			"---\ntitle: Diagram # ignored\n---\ngraph LR",
			"graph LR",
			"Diagram",
		},
		"leading blanks and indentation": {
			"\n  ---\n  title: Indented\n  ---\nsequenceDiagram",
			"sequenceDiagram",
			"Indented",
		},
		"unknown settings": {
			"---\nconfig:\n  theme: dark\ntitle: Known\n---\nerDiagram",
			"erDiagram",
			"Known",
		},
		"unclosed": {
			"---\ntitle: Incomplete\ngraph LR",
			"---\ntitle: Incomplete\ngraph LR",
			"",
		},
		"wrong closing indentation": {
			"  ---\n  title: Incomplete\n---\ngraph LR",
			"  ---\n  title: Incomplete\n---\ngraph LR",
			"",
		},
	} {
		body, title := StripFrontmatter(test.input)
		if body != test.wantBody || title != test.wantTitle {
			t.Errorf("%s: got body %q title %q, want body %q title %q", name, body, title, test.wantBody, test.wantTitle)
		}
	}
}

func TestCommentAndLineUtilitiesCoverEveryInputForm(t *testing.T) {
	lines := []string{"%% full", "one %% trailing", "", "  ", "two", "three %%"}
	if got, want := RemoveComments(lines), []string{"one", "two", "three"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}

	if got, want := SplitLines(`one\ntwo`+"\nthree"), []string{"one", "two", "three"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
