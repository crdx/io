package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/sandbox"
)

func TestTmpWouldShadowAWorkspace(t *testing.T) {
	for _, workspaceDir := range []string{sandbox.TmpDir, filepath.Join(sandbox.TmpDir, "project")} {
		if !workspacePathIsShadowed(workspaceDir) {
			t.Errorf("expected %q to be shadowed", workspaceDir)
		}
	}

	for _, workspaceDir := range []string{"/", "/tmp-project"} {
		if workspacePathIsShadowed(workspaceDir) {
			t.Errorf("did not expect %q to be shadowed", workspaceDir)
		}
	}
}

func TestAWorkspaceUnderTmpIsRefused(t *testing.T) {
	if err := ensureWorkspaceIsNotShadowed(t.TempDir()); !errors.Is(err, errWorkspaceShadowed) {
		t.Errorf("got %v, want the workspace shadowing error", err)
	}
}

func TestAWorkspaceOutsideTmpIsAccepted(t *testing.T) {
	if err := ensureWorkspaceIsNotShadowed("/"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAWorkspaceNamedThroughTmpIsRefused(t *testing.T) {
	alias := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink("/", alias); err != nil {
		t.Fatalf("could not create workspace alias: %v", err)
	}

	if err := ensureWorkspaceIsNotShadowed(alias); !errors.Is(err, errWorkspaceShadowed) {
		t.Errorf("got %v, want the workspace shadowing error", err)
	}
}

func TestARelativeXDGStateHomeIsIgnored(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join("inside", "workspace"))

	got := sessionsDir()
	if !filepath.IsAbs(got) {
		t.Fatalf("got relative state path %q", got)
	}
	if strings.HasPrefix(got, filepath.Join("inside", "workspace")) {
		t.Errorf("used the relative XDG_STATE_HOME in %q", got)
	}
}

func TestAnAbsoluteXDGStateHomeIsUsed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)

	want := filepath.Join(root, namespace, app, "sessions")
	if got := sessionsDir(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheConfigurationIsInTheXDGConfigDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	want := filepath.Join(root, namespace, app, "config.toml")
	if got := configPath(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheGlobalContextIsInTheXDGConfigDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	want := filepath.Join(root, namespace, app, "SYSTEM.md")
	if got := globalContextPath(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnExplicitMiseDataDirectoryIsPreserved(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MISE_DATA_DIR", dataDir)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if got := shellMiseDataDir(); got != dataDir {
		t.Errorf("got %q, want %q", got, dataDir)
	}
}

func TestMiseDataUsesTheOriginalXDGDataHome(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("MISE_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", dataHome)

	want := filepath.Join(dataHome, "mise")
	if got := shellMiseDataDir(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMiseDataFallsBackToTheOriginalHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MISE_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", home)

	want := filepath.Join(home, ".local", "share", "mise")
	if got := shellMiseDataDir(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
