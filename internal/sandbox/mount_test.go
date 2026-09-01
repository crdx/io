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
	probe := namespaceProbeCommand(t.Context())

	if !slices.Contains(probe.Args, "-test.run=^$") {
		t.Errorf("the namespace probe could recursively run tests: %v", probe.Args)
	}
	if !slices.Contains(probe.Env, envProbe+"=1") {
		t.Errorf("the namespace probe does not ask Init to handle it: %v", probe.Env)
	}
}

func TestEveryCommandGetsAMountNamespace(t *testing.T) {
	if namespaceAttributes().Cloneflags&syscall.CLONE_NEWNS == 0 {
		t.Error("expected every command to be given a mount namespace of its own")
	}
}

func TestTheVirtualResolverFilesAreDistinctAbsolutePathsWithContents(t *testing.T) {
	seen := make(map[string]struct{}, len(resolverFiles))

	for _, file := range resolverFiles {
		if !filepath.IsAbs(file.path) {
			t.Errorf("got resolver file %q, want an absolute path", file.path)
		}
		if _, isDuplicate := seen[file.path]; isDuplicate {
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

func TestEveryPolicyGrantsTheResolverFiles(t *testing.T) {
	var granted []string
	for _, grant := range (Policy{}).grants() {
		if !grant.isOptional {
			granted = append(granted, grant.path)
		}
	}

	for _, file := range resolverFiles {
		if !slices.Contains(granted, file.path) {
			t.Errorf("a policy granting nothing of its own lacks %s", file.path)
		}
	}
}

func TestEveryCommandGetsAPIDNamespace(t *testing.T) {
	attributes := namespaceAttributes()
	if attributes.Cloneflags&syscall.CLONE_NEWPID == 0 {
		t.Error("expected a PID namespace")
	}
	if attributes.Unshareflags != 0 {
		t.Error("expected namespaces to be created by clone")
	}
}

func TestEveryCommandOwnsItsProcessGroup(t *testing.T) {
	if !namespaceAttributes().Setpgid {
		t.Error("expected a command to own its process group")
	}
}

func TestEveryCommandDiesWithItsOwner(t *testing.T) {
	if namespaceAttributes().Pdeathsig != syscall.SIGKILL {
		t.Error("expected the command to be killed when its owner dies")
	}
}

func TestAScratchThatIsNotThereIsRefused(t *testing.T) {
	absent := Policy{TmpDir: "/scratch"}

	if missingPaths := absent.missingPaths(); len(missingPaths) != 1 || missingPaths[0] != "/scratch" {
		t.Errorf("expected the scratch to be reported missing, got %v", missingPaths)
	}
}

func TestEveryCommandGetsTheOtherNamespacesToo(t *testing.T) {
	attributes := namespaceAttributes()

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
