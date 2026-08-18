package sandbox

import (
	"syscall"
	"testing"
)

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
