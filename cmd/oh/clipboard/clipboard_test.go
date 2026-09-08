package clipboard

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/drops"
)

func TestMain(testMain *testing.M) {
	fixture := os.Getenv("IO_CLIPBOARD_FIXTURE")
	executable := filepath.Base(os.Args[0])
	if fixture != "" && (executable == "wl-paste" || executable == "xclip") {
		os.Exit(runClipboardFixture(fixture))
	}
	os.Exit(testMain.Run())
}

func runClipboardFixture(fixture string) int {
	isListing := slices.Contains(os.Args, "--list-types") || slices.Contains(os.Args, "TARGETS")
	switch fixture {
	case "wayland-image":
		if isListing {
			_, _ = os.Stdout.WriteString("text/plain\nimage/jpeg\nimage/png\n")
		} else {
			_, _ = os.Stdout.WriteString("png image bytes")
		}
	case "x-image":
		if isListing {
			_, _ = os.Stdout.WriteString("UTF8_STRING\nimage/webp\n")
		} else {
			_, _ = os.Stdout.WriteString("webp image bytes")
		}
	case "text":
		_, _ = os.Stdout.WriteString("text/plain\ntext/html\n")
	case "failed-read":
		if isListing {
			_, _ = os.Stdout.WriteString("image/png\n")
		} else {
			_, _ = os.Stderr.WriteString("clipboard owner disappeared")
			return 1
		}
	}
	return 0
}

func TestAWaylandClipboardImageIsSavedPrivately(t *testing.T) {
	binDirectory := t.TempDir()
	installClipboardExecutable(t, binDirectory, "wl-paste")
	t.Setenv("IO_CLIPBOARD_FIXTURE", "wayland-image")
	t.Setenv("PATH", binDirectory)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv("DISPLAY", "")

	sessionDirectory := filepath.Join(t.TempDir(), "brave-otter")
	wasEnsured := false
	path, err := SaveImage(sessionDirectory, func() error {
		wasEnsured = true
		return os.Mkdir(sessionDirectory, 0o700)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !wasEnsured {
		t.Error("image paste did not persist the session")
	}

	dropsDirectory := drops.GetDirectory(sessionDirectory)
	if filepath.Dir(path) != dropsDirectory || !strings.HasPrefix(filepath.Base(path), "image-") || filepath.Ext(path) != ".png" {
		t.Errorf("saved image path %q is not a PNG in %q", path, dropsDirectory)
	}
	dropsRoot, err := os.OpenRoot(dropsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dropsRoot.Close() }()
	content, err := dropsRoot.ReadFile(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "png image bytes" {
		t.Errorf("saved image reads %q", content)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("saved image mode is %o, want 600", fileInfo.Mode().Perm())
	}
}

func TestAnXClipboardImageIsSavedWhenWaylandIsUnavailable(t *testing.T) {
	binDirectory := t.TempDir()
	installClipboardExecutable(t, binDirectory, "xclip")
	t.Setenv("IO_CLIPBOARD_FIXTURE", "x-image")
	t.Setenv("PATH", binDirectory)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":1")

	sessionDirectory := t.TempDir()
	path, err := SaveImage(sessionDirectory, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(path) != ".webp" {
		t.Errorf("saved image path is %q, want WebP", path)
	}
}

func TestSupportedClipboardImagesKeepTheirFileType(t *testing.T) {
	for mediaType, extension := range map[string]string{
		"image/gif":  ".gif",
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/webp": ".webp",
	} {
		imageFormat, isFound := findImageFormat([]string{mediaType})
		if !isFound || imageFormat.extension != extension {
			t.Errorf("%s gave %+v and found=%t, want %s", mediaType, imageFormat, isFound, extension)
		}
	}
}

func TestTextOnTheClipboardIsNotSavedAsAnImage(t *testing.T) {
	binDirectory := t.TempDir()
	installClipboardExecutable(t, binDirectory, "wl-paste")
	t.Setenv("IO_CLIPBOARD_FIXTURE", "text")
	t.Setenv("PATH", binDirectory)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv("DISPLAY", "")

	sessionDirectory := t.TempDir()
	wasEnsured := false
	_, err := SaveImage(sessionDirectory, func() error {
		wasEnsured = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "does not contain") {
		t.Errorf("got %v, want no-image error", err)
	}
	if wasEnsured {
		t.Error("text paste persisted the session")
	}
	entries, err := os.ReadDir(sessionDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("text paste left %d files behind", len(entries))
	}
}

func TestAFailedClipboardReadLeavesNoFile(t *testing.T) {
	binDirectory := t.TempDir()
	installClipboardExecutable(t, binDirectory, "wl-paste")
	t.Setenv("IO_CLIPBOARD_FIXTURE", "failed-read")
	t.Setenv("PATH", binDirectory)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv("DISPLAY", "")

	sessionDirectory := t.TempDir()
	_, err := SaveImage(sessionDirectory, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "clipboard owner disappeared") {
		t.Errorf("got %v, want clipboard failure", err)
	}
	entries, err := os.ReadDir(drops.GetDirectory(sessionDirectory))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("failed paste left %d files behind", len(entries))
	}
}

func installClipboardExecutable(t *testing.T, directory string, name string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, filepath.Join(directory, name)); err != nil {
		t.Fatal(err)
	}
}
