package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const grantedVariable = "IO_SANDBOX_GRANTED"

func TestUnixSocketsAreIsolatedWhereLandlockCanEnforceIt(t *testing.T) {
	old := configuredRuleset(unixSocketsABI - 1)
	if old.handledAccessFS&accessResolveUnix != 0 || old.scopedRestrictions&scopeAbstractUnix != 0 {
		t.Error("an older ABI was configured with unsupported Unix socket isolation")
	}

	current := configuredRuleset(unixSocketsABI)
	if current.handledAccessFS&accessResolveUnix == 0 {
		t.Error("pathname Unix sockets were not isolated")
	}
	if current.scopedRestrictions&scopeAbstractUnix == 0 {
		t.Error("abstract Unix sockets were not isolated")
	}

	if rightsAtVersion(rightsWrite|accessResolveUnix, unixSocketsABI-1)&accessResolveUnix != 0 {
		t.Error("an older ABI was handed a right it would refuse the rule for")
	}
	if rightsAtVersion(rightsWrite|accessResolveUnix, unixSocketsABI)&accessResolveUnix == 0 {
		t.Error("a kernel that knows the right was not given it")
	}
}

func TestDeviceNodesAreHandledEverywhereAndGrantedNowhere(t *testing.T) {
	if rightsWrite&rightsDevice != 0 {
		t.Error("a writable path was granted the making of device nodes")
	}

	for _, rights := range []uint64{rightsRead, rightsExec, rightsFile} {
		if rights&rightsDevice != 0 {
			t.Errorf("rights %d were granted the making of device nodes", rights)
		}
	}

	if configuredRuleset(minABI).handledAccessFS&rightsDevice != rightsDevice {
		t.Error("the ruleset left the making of device nodes unhandled, which allows it everywhere")
	}
}

func TestAFileGrantReachesNothingAroundIt(t *testing.T) {
	if !insideChildProcess() {
		directory := t.TempDir()
		for _, name := range []string{"exact", "sibling"} {
			path := filepath.Join(directory, name)
			if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
				t.Fatal(err)
			}
		}

		runAgainInChildProcess(t, grantedVariable+"="+filepath.Join(directory, "exact"))
		return
	}

	exact := os.Getenv(grantedVariable)
	directory := filepath.Dir(exact)

	version, err := landlockVersion()
	if err != nil {
		t.Skipf("landlock cannot be asked here: %v", err)
	}

	if err := applyLandlock(grantingCoverage(Policy{Read: []string{exact}}), version); err != nil {
		t.Fatalf("could not enter the sandbox: %v", err)
	}

	content, err := os.ReadFile(exact) //nolint:gosec // the path is the one under test
	if err != nil || string(content) != "exact" {
		t.Errorf("the granted file got %q and %v", content, err)
	}

	//nolint:gosec // the path is the one under test
	if _, err := os.ReadFile(filepath.Join(directory, "sibling")); err == nil {
		t.Error("a sibling of the granted file was readable")
	}
	if _, err := os.ReadDir(directory); err == nil {
		t.Error("the directory holding the granted file was listable")
	}
	//nolint:gosec // the path is the one under test
	if err := os.WriteFile(exact, []byte("changed"), 0o600); err == nil {
		t.Error("a file granted for reading was writable")
	}
}

func TestAnOptionalPathTheMachineLacksDoesNotStopTheSandbox(t *testing.T) {
	if !insideChildProcess() {
		runAgainInChildProcess(t)
		return
	}

	version, err := landlockVersion()
	if err != nil {
		t.Skipf("landlock cannot be asked here: %v", err)
	}

	if err := applyLandlock(grantingCoverage(Policy{}), version); err != nil {
		t.Errorf("a policy naming only what the machine may have was refused: %v", err)
	}
}

func TestAPathThatIsNotThereIsNamedWhenItIsGranted(t *testing.T) {
	if !insideChildProcess() {
		runAgainInChildProcess(t, grantedVariable+"="+filepath.Join(t.TempDir(), "nowhere"))
		return
	}

	absent := os.Getenv(grantedVariable)

	version, err := landlockVersion()
	if err != nil {
		t.Skipf("landlock cannot be asked here: %v", err)
	}

	err = applyLandlock(grantingCoverage(Policy{Read: []string{absent}}), version)
	if err == nil {
		t.Fatal("a grant of a path that is not there was accepted")
	}
	if !strings.Contains(err.Error(), absent) {
		t.Errorf("got %v, want the path that could not be granted named", err)
	}
}

func TestAGrantThroughAModelSymlinkIsRefused(t *testing.T) {
	if !insideChildProcess() {
		writeRoot := t.TempDir()
		victim := t.TempDir()
		planted := filepath.Join(writeRoot, ".cache")
		if err := os.Symlink(victim, planted); err != nil {
			t.Fatal(err)
		}

		runAgainInChildProcess(t, grantedVariable+"="+writeRoot)
		return
	}

	writeRoot := os.Getenv(grantedVariable)
	planted := filepath.Join(writeRoot, ".cache")

	version, err := landlockVersion()
	if err != nil {
		t.Skipf("landlock cannot be asked here: %v", err)
	}

	err = applyLandlock(grantingCoverage(Policy{Write: []string{writeRoot, planted}}), version)
	if err == nil {
		t.Fatal("a grant through a symbolic link the model could have planted was accepted")
	}
	if !strings.Contains(err.Error(), planted) {
		t.Errorf("got %v, want the planted link named", err)
	}
}

func TestAGrantThroughAnAdminSymlinkIsFollowed(t *testing.T) {
	if !insideChildProcess() {
		root := t.TempDir()
		realDir := filepath.Join(root, "real")
		if err := os.Mkdir(realDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(realDir, "content"), []byte("visible"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(root, "scratch"), 0o700); err != nil {
			t.Fatal(err)
		}

		linked := filepath.Join(root, "linked")
		if err := os.Symlink(realDir, linked); err != nil {
			t.Fatal(err)
		}

		runAgainInChildProcess(t, grantedVariable+"="+linked)
		return
	}

	linked := os.Getenv(grantedVariable)
	root := filepath.Dir(linked)

	version, err := landlockVersion()
	if err != nil {
		t.Skipf("landlock cannot be asked here: %v", err)
	}

	policy := grantingCoverage(Policy{
		Read:  []string{linked},
		Write: []string{filepath.Join(root, "scratch")},
	})
	if err := applyLandlock(policy, version); err != nil {
		t.Fatalf("a grant through a link the model could not have planted was refused: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(linked, "content")) //nolint:gosec // the test's own link
	if err != nil || string(content) != "visible" {
		t.Errorf("the granted link got %q and %v", content, err)
	}
}

func TestAConfinedProcessCannotReadItsOwnProc(t *testing.T) {
	if !insideChildProcess() {
		runAgainInChildProcess(t)
		return
	}

	version, err := landlockVersion()
	if err != nil {
		t.Skipf("landlock cannot be asked here: %v", err)
	}

	if err := applyLandlock(grantingCoverage(Policy{}), version); err != nil {
		t.Fatalf("could not enter the sandbox: %v", err)
	}

	if _, err := os.ReadFile("/proc/self/status"); err == nil {
		t.Error("a confined process could read its own proc files")
	}
}

func TestAGrantedBinaryCanBeExecutedWithinTheSandbox(t *testing.T) {
	if !insideChildProcess() {
		runAgainInChildProcess(t)
		return
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	version, err := landlockVersion()
	if err != nil {
		t.Skipf("landlock cannot be asked here: %v", err)
	}

	policy := grantingCoverage(Policy{Exec: []string{self}})
	if err := applyLandlock(policy, version); err != nil {
		t.Fatalf("could not enter the sandbox: %v", err)
	}

	//nolint:gosec // the binary is this one, already running
	executed := exec.CommandContext(t.Context(), self, "-test.run=^$")
	executed.Env = []string{"PATH=" + os.Getenv("PATH")}

	if coverage := os.Getenv(coverageVariable); coverage != "" {
		executed.Env = append(executed.Env, "TMPDIR="+coverage, coverageVariable+"="+coverage)
	}

	if output, err := executed.CombinedOutput(); err != nil {
		t.Errorf("the confined process could not execute a granted binary: %v\n%s", err, output)
	}
}
