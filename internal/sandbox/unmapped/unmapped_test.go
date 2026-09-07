package unmapped_test

import (
	"os"
	"slices"
	"testing"

	"crdx.org/io/internal/sandbox/unmapped"
)

func canMapANamespace(t *testing.T) bool {
	t.Helper()

	file, err := os.OpenFile("/proc/self/uid_map", os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = file.Close()

	return true
}

func TestNothingIsCarriedForwardWhenTheVariableIsUnset(t *testing.T) {
	t.Setenv(unmapped.Variable, "")

	if environment := unmapped.Environment(); environment != nil {
		t.Errorf("got %q, want nothing carried into a child", environment)
	}
}

func TestTheVariableIsCarriedForwardWhenItIsSet(t *testing.T) {
	t.Setenv(unmapped.Variable, "1")

	environment := unmapped.Environment()
	if !slices.Contains(environment, unmapped.Variable+"=1") {
		t.Errorf("got %q, want the variable carried into a child", environment)
	}
}

func TestAnOrdinaryTestRunNeverStandsTheNamespacesDown(t *testing.T) {
	t.Setenv(unmapped.Variable, "")

	if unmapped.IsTestNamespace() {
		t.Error("got a stood-down namespace, want a test run asking for nothing to be left alone")
	}
}

func TestTheNamespaceStandsDownOnlyWhereItCannotBeMapped(t *testing.T) {
	t.Setenv(unmapped.Variable, "1")

	isStoodDown := unmapped.IsTestNamespace()

	if canMapANamespace(t) && isStoodDown {
		t.Error("got a stood-down namespace, want a machine that can map one to test for real")
	}
	if !canMapANamespace(t) && !isStoodDown {
		t.Error("got a mapped namespace, want a machine that cannot map one to stand down")
	}
}
