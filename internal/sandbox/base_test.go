package sandbox

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var neverGranted = []string{
	"/etc/shadow",
	"/etc/gshadow",
	"/etc/sudoers",
	"/etc/sudoers.d",
	"/etc/ssl/private",
	"/etc/ssh",
	"/etc/pam.d",
	"/etc/subuid",
	"/etc/subgid",
	"/etc/security",
	"/etc/environment",
	"/root",
}

func TestEnvironmentIsNotGrantedBecauseItCarriesTheUsersOwn(t *testing.T) {
	for _, granted := range systemPathGrants {
		if granted.path == "/etc/environment" {
			t.Error("/etc/environment is granted, and it can carry the user's own configuration")
		}
	}
}

func TestNothingSecretIsGrantedToEveryCommand(t *testing.T) {
	for _, secret := range neverGranted {
		for _, granted := range systemPathGrants {
			if granted.path == secret {
				t.Errorf("%s is granted to every command", secret)
			}

			if strings.HasPrefix(secret, granted.path+"/") {
				t.Errorf("%s grants %s, which is a secret", granted.path, secret)
			}
		}
	}
}

func TestEveryBaseGrantIsReadOnlyBeneathEtc(t *testing.T) {
	for _, granted := range systemPathGrants {
		if !strings.HasPrefix(granted.path, "/etc/") {
			continue
		}

		if granted.rights != rightsRead {
			t.Errorf("%s is granted more than reading", granted.path)
		}

		if !granted.isOptional {
			t.Errorf("%s is required, so a machine without it could run nothing", granted.path)
		}
	}
}

func TestNoBaseGrantIsNamedTwiceOrRelatively(t *testing.T) {
	seen := map[string]bool{}

	for _, granted := range systemPathGrants {
		if seen[granted.path] {
			t.Errorf("%s is granted twice", granted.path)
		}
		seen[granted.path] = true

		if !filepath.IsAbs(granted.path) || filepath.Clean(granted.path) != granted.path {
			t.Errorf("%s is not a clean absolute path", granted.path)
		}
	}
}

func TestBaselineReadablePathsMatchTheCommandsImplicitReadAccess(t *testing.T) {
	readable := BaselineReadablePaths()
	for _, granted := range systemPathGrants {
		isBaselineReadable := granted.rights&rightsRead == rightsRead
		if gotReadable := slices.Contains(readable, granted.path); gotReadable != isBaselineReadable {
			t.Errorf("%s listed=%t, want %t from rights %#x", granted.path, gotReadable, isBaselineReadable, granted.rights)
		}
	}
}
