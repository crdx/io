package sandbox

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const coverageVariable = "GOCOVERDIR"

func grantingCoverage(policy Policy) Policy {
	if directory := os.Getenv(coverageVariable); directory != "" {
		return policy.WithWrite(directory)
	}

	return policy
}

func insideChildProcess() bool {
	return os.Getenv(childVariable) != ""
}

func runAgainInChildProcess(t *testing.T, environment ...string) {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	coverage := t.TempDir()
	arguments := []string{"-test.run=^" + t.Name() + "$", "-test.v"}
	if testing.CoverMode() != "" {
		arguments = append(arguments, "-test.gocoverdir="+coverage)
	}

	child := exec.CommandContext(t.Context(), self, arguments...) //nolint:gosec // the binary is this one
	child.Env = append([]string{
		childVariable + "=1",
		coverageVariable + "=" + coverage,
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"TMPDIR=" + t.TempDir(),
	}, environment...)

	output, err := child.CombinedOutput()

	if reason, wasSkipped := skipReasonOf(string(output)); wasSkipped {
		t.Skip(reason)
	}

	if err != nil {
		t.Errorf("the confined child failed: %v\n%s", err, output)
	}
}

func withoutSourceLocation(line string) string {
	location, said, isLocated := strings.Cut(line, ": ")
	if isLocated && strings.Contains(location, ".go:") {
		return said
	}

	return line
}

func skipReasonOf(output string) (string, bool) {
	said := "the child skipped without saying why"

	for line := range strings.SplitSeq(output, "\n") {
		trimmedLine := strings.TrimSpace(line)

		if strings.HasPrefix(trimmedLine, "--- SKIP") {
			return said, true
		}

		if strings.HasPrefix(line, " ") && trimmedLine != "" {
			said = withoutSourceLocation(trimmedLine)
		}
	}

	return "", false
}

func TestAChildThatSkippedIsReadAsASkipRatherThanAPass(t *testing.T) {
	for _, test := range []struct {
		name       string
		output     string
		reason     string
		wasSkipped bool
	}{
		{
			name: "a skip carries the reason the child gave",
			output: "=== RUN   TestX\n" +
				"    seccomp_test.go:86: family 17 is already refused\n" +
				"--- SKIP: TestX (0.00s)\nPASS\n",
			reason:     "family 17 is already refused",
			wasSkipped: true,
		},
		{
			name:       "a skip with nothing said still reads as a skip",
			output:     "=== RUN   TestX\n--- SKIP: TestX (0.00s)\nPASS\n",
			reason:     "the child skipped without saying why",
			wasSkipped: true,
		},
		{
			name:   "a pass is not a skip",
			output: "=== RUN   TestX\n    seccomp_test.go:86: a note\n--- PASS: TestX (0.00s)\nPASS\n",
		},
		{
			name:   "a failure is not a skip",
			output: "=== RUN   TestX\n    seccomp_test.go:86: family 17 was allowed\n--- FAIL: TestX (0.00s)\nFAIL\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			reason, wasSkipped := skipReasonOf(test.output)
			if wasSkipped != test.wasSkipped {
				t.Fatalf("got %v, want %v", wasSkipped, test.wasSkipped)
			}
			if wasSkipped && reason != test.reason {
				t.Errorf("got %q, want %q", reason, test.reason)
			}
		})
	}
}
