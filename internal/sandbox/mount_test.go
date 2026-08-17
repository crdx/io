package sandbox

import (
	"syscall"
	"testing"
)

// A scratch is a mount, and a mount wants a namespace to make it in. A policy nesting nothing asks
// for no such namespace, so a machine that grants landlock and refuses CLONE_NEWNS keeps its shell
// where there is nothing to mount, and is told at the start rather than at the mount where there
// is.
func TestAScratchAsksForAMountNamespace(t *testing.T) {
	if namespaceAttributes(Policy{}).Unshareflags&syscall.CLONE_NEWNS != 0 {
		t.Error("expected a policy wanting no mount to ask for no mount namespace")
	}

	if namespaceAttributes(Policy{TmpDir: "/scratch"}).Unshareflags&syscall.CLONE_NEWNS == 0 {
		t.Error("expected a scratch to ask for a mount namespace")
	}
}

func TestBackgroundProcessesGetAPIDNamespace(t *testing.T) {
	attributes := namespaceAttributes(Policy{Background: true})
	if attributes.Cloneflags&syscall.CLONE_NEWPID == 0 {
		t.Error("expected background processes to ask for a PID namespace")
	}
	if attributes.Unshareflags != 0 {
		t.Error("expected the namespace init to be created by clone")
	}
	if attributes.Setpgid {
		t.Error("expected the namespace rather than a process group to own background processes")
	}
}

// A scratch that is not there cannot be mounted, and a caller believing a command has somewhere to
// write when it has not is worth interrupting, as a missing grant is.
func TestAScratchThatIsNotThereIsRefused(t *testing.T) {
	absent := Policy{TmpDir: "/scratch"}

	if missingPaths := absent.missingPaths(); len(missingPaths) != 1 || missingPaths[0] != "/scratch" {
		t.Errorf("expected the scratch to be reported missing, got %v", missingPaths)
	}
}
