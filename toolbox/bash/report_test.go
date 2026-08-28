package bash

import (
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"

	"crdx.org/io/internal/sandbox"

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

func killTestPolicy() sandbox.Policy {
	return sandbox.Policy{
		Timeout:  5 * time.Minute,
		CPUTime:  time.Hour,
		FileSize: 1024 << 20,
		Write:    []string{"/workspace"},
	}
}

func TestACommandKilledForItsProcessorTimeIsToldTheLimitAndWhatItUsed(t *testing.T) {
	result := sandbox.Result{
		Code:    137,
		CPUTime: 90 * time.Minute,
		Output:  "error: recipe `lint1` was terminated on line 104 by signal 9",
	}

	got := report(result, killTestPolicy())

	for _, want := range []string{
		"killed by SIGKILL",
		"each process 1h00m of processor time",
		"counted across every thread it runs",
		"after 5m00s of wall clock",
		"used 1h30m of processor time between them",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("got %q, want it to mention %q", got, want)
		}
	}
}

func TestACommandKilledForWritingTooMuchIsToldTheFileLimit(t *testing.T) {
	result := sandbox.Result{Code: 153, Output: "dd: writing 'big': File size limit exceeded"}

	got := report(result, killTestPolicy())

	if !strings.Contains(got, "killed by SIGXFSZ") || !strings.Contains(got, "no more than 1G") {
		t.Errorf("got %q, want the signal and the file size limit named", got)
	}
	if strings.Contains(got, "using too much") {
		t.Errorf("got %q, want the vague note replaced by the exact one", got)
	}
}

func TestAKillIsReportedEvenWhereTheOutputLooksLikeADenial(t *testing.T) {
	result := sandbox.Result{Code: 137, Output: "cp: cannot open 'x': Permission denied"}

	if got := report(result, killTestPolicy()); !strings.Contains(got, "killed by SIGKILL") {
		t.Errorf("got %q, want a kill reported whatever else the output says", got)
	}
}

func TestAnUnkilledFailureIsAccusedOfNothing(t *testing.T) {
	for name, code := range map[string]int{
		"an ordinary failure":       1,
		"a status of the boundary":  128,
		"a status above any signal": 255,
	} {
		got := report(sandbox.Result{Code: code, Output: "make: *** [all] Error 1"}, killTestPolicy())
		if strings.Contains(got, "killed by") {
			t.Errorf("%s: got %q, want no kill claimed", name, got)
		}
	}
}

func TestASignalTheCommandItselfDiedOfIsReportedWithoutHedging(t *testing.T) {
	result := sandbox.Result{
		Code:    -1,
		Signal:  syscall.SIGKILL,
		CPUTime: 90 * time.Minute,
	}

	got := report(result, killTestPolicy())

	if !strings.Contains(got, "note: the command was killed by SIGKILL.") {
		t.Errorf("got %q, want an observed kill stated outright", got)
	}
	if strings.Contains(got, "the shell reports") {
		t.Errorf("got %q, want no hedge where the kill was seen rather than inferred", got)
	}
	if !strings.Contains(got, "each process 1h00m of processor time") {
		t.Errorf("got %q, want the processor limit named", got)
	}
}

func TestASignalOnlyTheShellSawIsReportedAsSuch(t *testing.T) {
	result := sandbox.Result{Code: 137, Output: "Killed"}

	got := report(result, killTestPolicy())

	if !strings.Contains(got, "note: the shell reports that a process was killed by SIGKILL.") {
		t.Errorf("got %q, want an inferred kill attributed to the shell", got)
	}
}

func TestAnObservedSignalIsPreferredToTheExitStatus(t *testing.T) {
	result := sandbox.Result{Code: 139, Signal: syscall.SIGXFSZ}

	got := report(result, killTestPolicy())

	if !strings.Contains(got, "killed by SIGXFSZ") || strings.Contains(got, "SIGSEGV") {
		t.Errorf("got %q, want what was seen rather than what the status implies", got)
	}
}

func TestAKillUnderNoProcessorLimitNamesTheSignalAlone(t *testing.T) {
	result := sandbox.Result{Code: 137, Output: "Killed"}

	got := report(result, sandbox.Policy{Timeout: time.Minute})

	if !strings.Contains(got, "killed by SIGKILL") {
		t.Errorf("got %q, want the signal named", got)
	}
	if strings.Contains(got, "processor time") {
		t.Errorf("got %q, want no limit named where the policy sets none", got)
	}
}
