package sandbox

import (
	"path/filepath"
	"slices"
	"testing"
)

func rightsFor(grants []grant, path string) (uint64, bool) {
	for _, granted := range grants {
		if granted.path == path {
			return granted.rights, true
		}
	}

	return 0, false
}

func TestAPolicyIsWritableOnlyBeyondItsScratch(t *testing.T) {
	if (Policy{}).Writable() {
		t.Error("a policy granting nothing was called writable")
	}
	if (Policy{Write: []string{TmpDir}}).Writable() {
		t.Error("a policy granting only its scratch was called writable")
	}
	if !(Policy{Write: []string{TmpDir, "/elsewhere"}}).Writable() {
		t.Error("a policy granting a path of its own was not called writable")
	}
}

func TestEveryPolicyGrantsWhatACommandNeedsToStart(t *testing.T) {
	grants := Policy{}.grants()

	for _, path := range []string{"/usr", "/lib", "/etc/passwd", "/dev/null", "/proc/self"} {
		rights, granted := rightsFor(grants, path)
		if !granted {
			t.Errorf("%s was not granted at all", path)
			continue
		}
		if rights&rightsRead != rightsRead {
			t.Errorf("%s got rights %#x, want it readable", path, rights)
		}
	}

	if rights, _ := rightsFor(grants, "/usr"); rights&accessExecute == 0 {
		t.Error("commands were granted but not made executable")
	}
	if rights, _ := rightsFor(grants, "/dev/null"); rights&accessWriteFile == 0 {
		t.Error("output cannot be thrown away")
	}
	if _, granted := rightsFor(grants, "/etc"); granted {
		t.Error("the whole of /etc was granted")
	}
}

func TestEveryPolicyCanReadSystemFontConfiguration(t *testing.T) {
	rights, granted := rightsFor(Policy{}.grants(), "/etc/fonts")
	if !granted {
		t.Fatal("system font configuration was not granted")
	}
	if rights != rightsRead {
		t.Errorf("system font configuration got rights %#x, want %#x", rights, rightsRead)
	}
}

func TestWhatAPolicyNamesIsGrantedWithTheRightsItAsked(t *testing.T) {
	grants := Policy{
		Read:  []string{"/named-read"},
		Exec:  []string{"/named-exec"},
		Write: []string{"/named-write"},
	}.grants()

	for path, want := range map[string]uint64{
		"/named-read": rightsRead, "/named-exec": rightsExec, "/named-write": rightsWrite,
	} {
		rights, granted := rightsFor(grants, path)
		if !granted {
			t.Errorf("%s was not granted at all", path)
			continue
		}
		if rights != want {
			t.Errorf("%s got rights %#x, want %#x", path, rights, want)
		}
	}
}

func TestOnlyAPolicyWithItsOwnMountsGrantsPseudoterminals(t *testing.T) {
	if _, granted := rightsFor(Policy{}.grants(), "/dev/ptmx"); granted {
		t.Error("a policy without mounts of its own granted the pseudoterminal multiplexer")
	}

	grants := Policy{TmpDir: "/scratch"}.grants()
	for _, path := range []string{"/dev/ptmx", "/dev/pts"} {
		rights, granted := rightsFor(grants, path)
		if !granted {
			t.Errorf("%s was not granted to a policy that mounts its own devices", path)
			continue
		}
		if rights != rightsWrite {
			t.Errorf("%s got rights %#x, want it writable", path, rights)
		}
	}
}

func TestWhatTheSystemMayNotHaveIsOptionalAndWhatThePolicyNamesIsNot(t *testing.T) {
	for _, granted := range base {
		if !granted.isOptional {
			t.Errorf("%s is required, so a machine without it could not run a command", granted.path)
		}
	}

	for _, granted := range (Policy{Read: []string{"/named-read"}}).grants() {
		if granted.path == "/named-read" && granted.isOptional {
			t.Error("a path the policy named was treated as optional")
		}
	}
}

func TestOnlyAReadPathInsideAWritePathIsNested(t *testing.T) {
	inside := Policy{Read: []string{"/work/held"}, Write: []string{"/work"}}
	if !slices.Equal(inside.nestedPaths(), []string{"/work/held"}) {
		t.Errorf("got %v, want the read path within the write path", inside.nestedPaths())
	}
	if !inside.usesMountNamespace() {
		t.Error("a nested path needs a mount namespace to be held read-only")
	}

	outside := Policy{Read: []string{"/elsewhere"}, Write: []string{"/work"}}
	if len(outside.nestedPaths()) != 0 {
		t.Errorf("got %v, want nothing nested", outside.nestedPaths())
	}
	if outside.usesMountNamespace() {
		t.Error("a policy with nothing to mount asked for a mount namespace")
	}
}

func TestAPathTheMachineLacksIsNamedOnceAndOnlyWhenItIsRequired(t *testing.T) {
	directory := t.TempDir()
	absent := filepath.Join(directory, "nowhere")

	missing := Policy{Read: []string{absent, directory}}.missingPaths()
	if !slices.Equal(missing, []string{absent}) {
		t.Errorf("got %v, want only the path that is not there", missing)
	}

	if missing := (Policy{Read: []string{directory}}).missingPaths(); len(missing) != 0 {
		t.Errorf("got %v, want a policy naming what exists to be accepted", missing)
	}
}
