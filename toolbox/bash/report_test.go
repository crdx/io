package bash

import (
	"errors"
	"testing"

	"crdx.org/io/tool"
)

func TestAnUnfinishedCommandKeepsItsOutputAndSaysWhyItEnded(t *testing.T) {
	stopped := errors.New("the command was stopped after 12s because the user pressed escape")

	tests := map[string]struct {
		output string
		err    error
		want   string
	}{
		"output and a stop": {
			output: "compiling\n",
			err:    stopped,
			want:   "compiling\nnote: the command was stopped after 12s because the user pressed escape.",
		},
		"trailing blank lines": {
			output: "compiling\n\n\n",
			err:    stopped,
			want:   "compiling\nnote: the command was stopped after 12s because the user pressed escape.",
		},
		"a timeout": {
			output: "compiling\n",
			err:    errors.New("the command did not finish within 2m0s"),
			want:   "compiling\nnote: the command did not finish within 2m0s.",
		},
		"nothing written": {
			output: "",
			err:    stopped,
			want:   "",
		},
		"nothing but whitespace": {
			output: " \n\t\n",
			err:    stopped,
			want:   "",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := unfinished(test.output, test.err); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestWhatACommandProducedIsMeasured(t *testing.T) {
	stats := tool.Stats{Kind: tool.StatsResources}

	if got := measured("one\ntwo\n", &stats); got != "one\ntwo\n" {
		t.Errorf("got %q, want the text back unchanged", got)
	}
	if stats.Lines != 2 || stats.Bytes != 8 || stats.TotalBytes != 8 {
		t.Errorf("got %+v, want 2 lines and 8 bytes", stats)
	}
	if stats.Kind != tool.StatsResources {
		t.Errorf("got kind %q, want what a command reports", stats.Kind)
	}
}

func TestNothingProducedIsMeasuredAsNothing(t *testing.T) {
	stats := tool.Stats{Kind: tool.StatsResources}

	measured("", &stats)

	if stats.Lines != 0 || stats.Bytes != 0 {
		t.Errorf("got %+v, want an empty report to measure as empty", stats)
	}
}
