package sandbox

import (
	"os"
	"slices"
	"syscall"
	"testing"
)

func TestTheNamespaceProbeCannotRunTestsIfInitIsMissing(t *testing.T) {
	probe := namespaceProbeCommand(t.Context(), Policy{})

	if !slices.Contains(probe.Args, "-test.run=^$") {
		t.Errorf("the namespace probe could recursively run tests: %v", probe.Args)
	}
	if !slices.Contains(probe.Env, envProbe+"=1") {
		t.Errorf("the namespace probe does not ask Init to handle it: %v", probe.Env)
	}
}

func TestAScratchAsksForAMountNamespace(t *testing.T) {
	if namespaceAttributes(Policy{}).Cloneflags&syscall.CLONE_NEWNS != 0 {
		t.Error("expected a policy wanting no mount to ask for no mount namespace")
	}

	if namespaceAttributes(Policy{TmpDir: "/scratch"}).Cloneflags&syscall.CLONE_NEWNS == 0 {
		t.Error("expected a scratch to ask for a mount namespace")
	}
}

func TestEveryCommandGetsAPIDNamespace(t *testing.T) {
	for _, background := range []bool{false, true} {
		attributes := namespaceAttributes(Policy{Background: background})
		if attributes.Cloneflags&syscall.CLONE_NEWPID == 0 {
			t.Errorf("background %t: expected a PID namespace", background)
		}
		if attributes.Unshareflags != 0 {
			t.Errorf("background %t: expected namespaces to be created by clone", background)
		}
	}
}

func TestOnlyForegroundCommandsGetAProcessGroup(t *testing.T) {
	if !namespaceAttributes(Policy{}).Setpgid {
		t.Error("expected a foreground command to own its process group")
	}
	if namespaceAttributes(Policy{Background: true}).Setpgid {
		t.Error("expected the namespace rather than a process group to own background processes")
	}
}

func TestAScratchThatIsNotThereIsRefused(t *testing.T) {
	absent := Policy{TmpDir: "/scratch"}

	if missingPaths := absent.missingPaths(); len(missingPaths) != 1 || missingPaths[0] != "/scratch" {
		t.Errorf("expected the scratch to be reported missing, got %v", missingPaths)
	}
}

func TestAPolicyWithNoMountsOfItsOwnRearrangesNothing(t *testing.T) {
	if err := applyMounts(Policy{Read: []string{"/elsewhere"}, Write: []string{"/work"}}); err != nil {
		t.Errorf("got %v, want a policy with nothing nested to leave the mounts alone", err)
	}
}

func TestAPolicyWithNothingNestedStillGetsTheOtherNamespaces(t *testing.T) {
	attributes := namespaceAttributes(Policy{})

	for name, flag := range map[string]uintptr{
		"user":    syscall.CLONE_NEWUSER,
		"network": syscall.CLONE_NEWNET,
		"pid":     syscall.CLONE_NEWPID,
	} {
		if attributes.Cloneflags&flag == 0 {
			t.Errorf("expected a %s namespace", name)
		}
	}

	if len(attributes.UidMappings) != 1 || attributes.UidMappings[0].HostID != os.Getuid() {
		t.Errorf("got %v, want the caller mapped to root of the namespace", attributes.UidMappings)
	}
	if len(attributes.GidMappings) != 1 || attributes.GidMappings[0].HostID != os.Getgid() {
		t.Errorf("got %v, want the caller's group mapped to root of the namespace", attributes.GidMappings)
	}
}
