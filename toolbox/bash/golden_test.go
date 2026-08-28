package bash

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"crdx.org/io/internal/sandbox"
)

var updateGoldens = flag.Bool("update", false, "write what was reported back to the golden files")

func compareWithGolden(t *testing.T, name string, reported string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)

	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(reported), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}

	if reported != string(want) {
		t.Errorf("report differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, reported, want)
	}
}

func goldenPolicy() sandbox.Policy {
	return sandbox.Policy{
		Timeout:  5 * time.Minute,
		CPUTime:  time.Hour,
		FileSize: 1024 << 20,
		Read:     []string{"/etc/ssl/certs", "/nix/store"},
		Write:    []string{"/workspace", "/tmp"},
	}
}

func TestEveryWholeReportACommandCanEndOnMatchesTheGolden(t *testing.T) {
	reports := []struct {
		name   string
		result sandbox.Result
		policy sandbox.Policy
	}{
		{
			name:   "a command that succeeded",
			result: sandbox.Result{Code: 0, Output: "cmd/oh/main.go\n"},
			policy: goldenPolicy(),
		},
		{
			name:   "a command that failed on its own",
			result: sandbox.Result{Code: 1, Output: "make: *** [all] Error 1"},
			policy: goldenPolicy(),
		},
		{
			name:   "a command that failed silently",
			result: sandbox.Result{Code: 2},
			policy: goldenPolicy(),
		},
		{
			name: "a kill the sandbox saw itself",
			result: sandbox.Result{
				Code:    -1,
				Signal:  syscall.SIGKILL,
				CPUTime: 90 * time.Minute,
			},
			policy: goldenPolicy(),
		},
		{
			name: "a kill only the shell saw",
			result: sandbox.Result{
				Code:    137,
				Output:  "error: recipe `lint` was terminated on line 104 by signal 9",
				CPUTime: 61*time.Minute + 24*time.Second,
			},
			policy: goldenPolicy(),
		},
		{
			name: "a kill for writing too large a file",
			result: sandbox.Result{
				Code:   153,
				Output: "dd: writing 'big': File size limit exceeded",
			},
			policy: goldenPolicy(),
		},
		{
			name:   "a kill under a policy that sets no processor limit",
			result: sandbox.Result{Code: 137, Output: "Killed"},
			policy: sandbox.Policy{Timeout: time.Minute, Write: []string{"/workspace"}},
		},
		{
			name: "a refusal the sandbox made",
			result: sandbox.Result{
				Code:   1,
				Output: "cp: cannot create regular file '/etc/hosts': Permission denied",
			},
			policy: goldenPolicy(),
		},
		{
			name: "a write a read-only mount refused, which no note is made of",
			result: sandbox.Result{
				Code:   1,
				Output: "cp: cannot create regular file '/etc/hosts': Read-only file system",
			},
			policy: goldenPolicy(),
		},
		{
			name: "a refusal under a policy granting nothing to read",
			result: sandbox.Result{
				Code:   1,
				Output: "cat: /etc/shadow: Permission denied",
			},
			policy: sandbox.Policy{Timeout: time.Minute, Write: []string{"/workspace"}},
		},
		{
			name: "a refusal that reads like a kill",
			result: sandbox.Result{
				Code:    137,
				Output:  "cp: cannot open 'x': Permission denied",
				CPUTime: 3 * time.Second,
			},
			policy: goldenPolicy(),
		},
		{
			name: "a command that overran",
			result: sandbox.Result{
				Code:   1,
				Output: "go: writing cache: open /cache/go-build/x: Too many open files",
			},
			policy: goldenPolicy(),
		},
	}

	var written strings.Builder

	for _, reported := range reports {
		fmt.Fprintf(&written, "=== %s ===\n%s\n\n", reported.name, report(reported.result, reported.policy))
	}

	compareWithGolden(t, "report.txt", strings.TrimSuffix(written.String(), "\n"))
}
