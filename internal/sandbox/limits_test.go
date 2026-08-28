package sandbox

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestALimitThatIsNotALimitIsRefused(t *testing.T) {
	for _, test := range []struct {
		name   string
		policy Policy
		want   string
	}{
		{name: "a negative file size", policy: Policy{FileSize: -1}, want: "is not a size"},
		{name: "a negative file count", policy: Policy{OpenFiles: -1}, want: "is not a count"},
		{name: "a negative process count", policy: Policy{Processes: -1}, want: "is not a count"},
		{name: "a sub-second cpu limit", policy: Policy{CPUTime: time.Millisecond}, want: "no time at all"},
	} {
		err := test.policy.sane()
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: got %v, want a complaint mentioning %q", test.name, err, test.want)
		}
	}
}

func TestALimitThatCanBeMetIsAccepted(t *testing.T) {
	policy := Policy{FileSize: 1024, OpenFiles: 64, Processes: 64, CPUTime: time.Second}

	if err := policy.sane(); err != nil {
		t.Errorf("got %v, want a policy that can be met", err)
	}
}

func TestAScratchCoveringAGrantIsRefusedAndTheScratchItselfIsNot(t *testing.T) {
	covered := Policy{TmpDir: "/scratch", Read: []string{TmpDir + "/elsewhere"}}
	if err := covered.reachable(); err == nil || !strings.Contains(err.Error(), "granted but unreachable") {
		t.Errorf("got %v, want the covered grant to be named", err)
	}

	itself := Policy{TmpDir: "/scratch", Write: []string{TmpDir}}
	if err := itself.reachable(); err != nil {
		t.Errorf("got %v, want the scratch itself to be granted", err)
	}

	elsewhere := Policy{Read: []string{TmpDir + "/elsewhere"}}
	if err := elsewhere.reachable(); err != nil {
		t.Errorf("got %v, want a policy without a scratch to grant what it likes", err)
	}
}

const coverableFileSize = 64 << 20

func TestTheLimitsAPolicyNamesAreTheOnesTheCommandGets(t *testing.T) {
	if !insideChildProcess() {
		runAgainInChildProcess(t)
		return
	}

	policy := Policy{
		CPUTime:   30 * time.Second,
		FileSize:  coverableFileSize,
		OpenFiles: 64,
		Processes: 128,
	}
	if err := applyLimits(policy); err != nil {
		t.Fatalf("could not apply the limits: %v", err)
	}

	for _, test := range []struct {
		name     string
		resource int
		want     uint64
	}{
		{name: "core", resource: unix.RLIMIT_CORE, want: 0},
		{name: "processor time", resource: unix.RLIMIT_CPU, want: 30},
		{name: "file size", resource: unix.RLIMIT_FSIZE, want: coverableFileSize},
		{name: "open files", resource: unix.RLIMIT_NOFILE, want: 64},
		{name: "processes", resource: unix.RLIMIT_NPROC, want: 128},
	} {
		var limit unix.Rlimit
		if err := unix.Getrlimit(test.resource, &limit); err != nil {
			t.Fatalf("%s: could not read the limit: %v", test.name, err)
		}
		if limit.Cur != test.want || limit.Max != test.want {
			t.Errorf("%s: got %d and %d, want both to be %d", test.name, limit.Cur, limit.Max, test.want)
		}
	}
}

func TestAPolicyNamingNoLimitsLeavesThemAloneApartFromCores(t *testing.T) {
	if !insideChildProcess() {
		runAgainInChildProcess(t)
		return
	}

	untouched := []struct {
		name     string
		resource int
	}{
		{name: "descriptor", resource: unix.RLIMIT_NOFILE},
		{name: "process", resource: unix.RLIMIT_NPROC},
	}

	before := make([]unix.Rlimit, len(untouched))
	for i, limit := range untouched {
		if err := unix.Getrlimit(limit.resource, &before[i]); err != nil {
			t.Fatalf("%s: could not read the limit: %v", limit.name, err)
		}
	}

	if err := applyLimits(Policy{}); err != nil {
		t.Fatalf("could not apply the limits: %v", err)
	}

	for i, limit := range untouched {
		var after unix.Rlimit
		if err := unix.Getrlimit(limit.resource, &after); err != nil {
			t.Fatalf("%s: could not read the limit: %v", limit.name, err)
		}
		if after != before[i] {
			t.Errorf("got %+v, want the %s limit left at %+v", after, limit.name, before[i])
		}
	}

	var cores unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CORE, &cores); err != nil {
		t.Fatalf("could not read the limit: %v", err)
	}
	if cores.Cur != 0 || cores.Max != 0 {
		t.Errorf("got core limit %+v, want no core dump of a confined command", cores)
	}
}
