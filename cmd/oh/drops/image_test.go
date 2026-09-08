package drops_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/drops"
)

func persisted(directory string) func() error {
	return func() error { return os.MkdirAll(directory, 0o700) }
}

func TestAPastedImageIsWrittenIntoTheDropsDirectory(t *testing.T) {
	sessionDirectory := filepath.Join(t.TempDir(), "brave-otter")

	path, err := drops.SaveImage(
		sessionDirectory,
		persisted(sessionDirectory),
		"image/png",
		[]byte("\x89PNG\r\n"),
	)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := filepath.Dir(path), drops.GetDirectory(sessionDirectory); got != want {
		t.Errorf("image was written to %q, want it under %q", got, want)
	}
	if got := filepath.Base(path); !strings.HasPrefix(got, "image-") || !strings.HasSuffix(got, ".png") {
		t.Errorf("image is named %q, want an image-*.png", got)
	}

	written, err := os.ReadFile(path) //nolint:gosec // a path this test just created
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "\x89PNG\r\n" {
		t.Errorf("image holds %q, want the pasted bytes", written)
	}
}

func TestEachSupportedImageTypeTakesItsOwnExtension(t *testing.T) {
	for mediaType, want := range map[string]string{
		"image/png":  ".png",
		"image/jpeg": ".jpg",
		"image/webp": ".webp",
		"image/gif":  ".gif",
	} {
		t.Run(mediaType, func(t *testing.T) {
			sessionDirectory := filepath.Join(t.TempDir(), "brave-otter")

			path, err := drops.SaveImage(
				sessionDirectory,
				persisted(sessionDirectory),
				mediaType,
				[]byte("data"),
			)
			if err != nil {
				t.Fatal(err)
			}

			if got := filepath.Ext(path); got != want {
				t.Errorf("extension is %q, want %q", got, want)
			}
		})
	}
}

func TestAnUnsupportedImageTypeIsRefusedWithoutPreparingAnything(t *testing.T) {
	sessionDirectory := filepath.Join(t.TempDir(), "brave-otter")

	_, err := drops.SaveImage(
		sessionDirectory,
		persisted(sessionDirectory),
		"image/tiff",
		[]byte("II*\x00"),
	)
	if err == nil {
		t.Fatal("an unsupported image type was accepted")
	}
	if !strings.Contains(err.Error(), "image/tiff") {
		t.Errorf("error is %q, want it to name the type", err)
	}
	if _, err := os.Stat(drops.GetDirectory(sessionDirectory)); !os.IsNotExist(err) {
		t.Error("a refused image prepared the drops directory anyway")
	}
}
