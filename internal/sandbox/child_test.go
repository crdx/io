package sandbox

import (
	"os"
	"os/exec"
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

	child := exec.Command(self, arguments...) //nolint:gosec // the binary is this one
	child.Env = append([]string{
		childVariable + "=1",
		coverageVariable + "=" + coverage,
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"TMPDIR=" + t.TempDir(),
	}, environment...)

	if output, err := child.CombinedOutput(); err != nil {
		t.Errorf("the confined child failed: %v\n%s", err, output)
	}
}
