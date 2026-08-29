package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestAnythingMountedAsksForAMountNamespace(t *testing.T) {
	if namespaceAttributes(Policy{}).Cloneflags&syscall.CLONE_NEWNS != 0 {
		t.Error("expected a policy wanting no mount to ask for no mount namespace")
	}

	if namespaceAttributes(Policy{TmpDir: "/scratch"}).Cloneflags&syscall.CLONE_NEWNS == 0 {
		t.Error("expected a scratch to ask for a mount namespace")
	}

	privateProcessFilesystem := Policy{UseProcFS: true}
	if namespaceAttributes(privateProcessFilesystem).Cloneflags&syscall.CLONE_NEWNS == 0 {
		t.Error("expected a private process filesystem to ask for a mount namespace")
	}

	virtual := Policy{UseVirtualResolver: true}
	if namespaceAttributes(virtual).Cloneflags&syscall.CLONE_NEWNS == 0 {
		t.Error("expected virtual resolver configuration to ask for a mount namespace")
	}
}

func TestTheVirtualResolverFilesAreDistinctAbsolutePathsWithContents(t *testing.T) {
	seen := make(map[string]struct{}, len(resolverFiles))

	for _, file := range resolverFiles {
		if !filepath.IsAbs(file.path) {
			t.Errorf("got resolver file %q, want an absolute path", file.path)
		}
		if _, duplicate := seen[file.path]; duplicate {
			t.Errorf("resolver file %q is mounted twice", file.path)
		}
		seen[file.path] = struct{}{}

		if !strings.HasSuffix(file.contents, "\n") {
			t.Errorf("the contents of %q do not end in a newline", file.path)
		}
	}

	if len(resolverFiles) != 3 {
		t.Errorf("got %d resolver files, want the three of the resolver stack", len(resolverFiles))
	}
}

func TestOnlyAPolicyAskingForItGrantsTheResolverFiles(t *testing.T) {
	granted := func(policy Policy) []string {
		var paths []string
		for _, grant := range policy.grants() {
			if !grant.isOptional {
				paths = append(paths, grant.path)
			}
		}
		return paths
	}

	for _, file := range resolverFiles {
		if slices.Contains(granted(Policy{}), file.path) {
			t.Errorf("a policy asking for nothing was granted %s", file.path)
		}
		if !slices.Contains(granted(Policy{UseVirtualResolver: true}), file.path) {
			t.Errorf("a policy with virtual resolver configuration lacks %s", file.path)
		}
	}
}

func TestEveryCommandGetsAPIDNamespace(t *testing.T) {
	attributes := namespaceAttributes(Policy{})
	if attributes.Cloneflags&syscall.CLONE_NEWPID == 0 {
		t.Error("expected a PID namespace")
	}
	if attributes.Unshareflags != 0 {
		t.Error("expected namespaces to be created by clone")
	}
}

func TestEveryCommandOwnsItsProcessGroup(t *testing.T) {
	if !namespaceAttributes(Policy{}).Setpgid {
		t.Error("expected a command to own its process group")
	}
}

func TestEveryCommandDiesWithItsOwner(t *testing.T) {
	if namespaceAttributes(Policy{}).Pdeathsig != syscall.SIGKILL {
		t.Error("expected the command to be killed when its owner dies")
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
