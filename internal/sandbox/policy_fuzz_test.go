package sandbox

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const (
	fuzzedPathCount  = 6
	fuzzedPathLength = 64
)

func fuzzedPaths(t *testing.T, encoded string) []string {
	t.Helper()

	if encoded == "" {
		return nil
	}

	paths := strings.Split(encoded, "\n")
	if len(paths) > fuzzedPathCount {
		t.Skip("more paths than a policy is ever built with")
	}

	for _, path := range paths {
		if len(path) > fuzzedPathLength {
			t.Skip("a path longer than a policy is ever built with")
		}
	}

	return paths
}

func FuzzAPolicyMeansTheSameThingOnBothSidesOfTheProcessBoundary(fuzzer *testing.F) {
	for _, seed := range []struct {
		read    string
		write   string
		exec    string
		sockets string
		scratch string
	}{
		{},
		{read: "/read", write: "/write", exec: "/exec"},
		{read: "/tmp/held", write: "/tmp", sockets: "/tmp", scratch: "/scratch"},
		{read: "/work/held", write: "/work"},
		{read: "/a\xffb"},
		{read: "/a\x00b"},
		{read: "/a\nb", write: "/a"},
		{read: "relative/path"},
		{read: "/a/../b"},
		{scratch: "/tmp"},
	} {
		fuzzer.Add(seed.read, seed.write, seed.exec, seed.sockets, seed.scratch)
	}

	fuzzer.Fuzz(func(
		t *testing.T,
		read string,
		write string,
		exec string,
		sockets string,
		scratch string,
	) {
		if len(scratch) > fuzzedPathLength {
			t.Skip("a scratch longer than a policy is ever built with")
		}

		validated := Policy{
			Read:    fuzzedPaths(t, read),
			Write:   fuzzedPaths(t, write),
			Exec:    fuzzedPaths(t, exec),
			Sockets: fuzzedPaths(t, sockets),
			TmpDir:  scratch,
		}

		if validated.sane() != nil {
			t.Skip("a policy the parent refuses never reaches the child")
		}

		encoded, err := json.Marshal(validated)
		if err != nil {
			t.Fatalf("the policy could not be written: %v", err)
		}

		if bytes.ContainsRune(encoded, 0) {
			t.Fatalf("an accepted policy cannot be carried in an environment variable: %s", encoded)
		}

		var enforced Policy
		if err := json.Unmarshal(encoded, &enforced); err != nil {
			t.Fatalf("the policy could not be read back: %v", err)
		}

		if !reflect.DeepEqual(validated, enforced) {
			t.Fatalf("the child would enforce %+v where the parent validated %+v", enforced, validated)
		}
	})
}
