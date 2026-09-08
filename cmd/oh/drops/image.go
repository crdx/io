package drops

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

var imageExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

const (
	nameDigestLength = 16
	maxNameAttempts  = 100
)

func SaveImage(sessionDirectory string, ensureSession func() error, mediaType string, data []byte) (string, error) {
	extension, isKnown := imageExtensions[mediaType]
	if !isKnown {
		return "", fmt.Errorf("the clipboard holds an unsupported image type %q", mediaType)
	}

	directory, err := Prepare(sessionDirectory, ensureSession)
	if err != nil {
		return "", err
	}

	path, isDropped, err := imagePath(directory, data, extension)
	if err != nil {
		return "", err
	}
	if isDropped {
		return path, nil
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write the image file: %w", err)
	}

	return path, nil
}

func imagePath(directory string, data []byte, extension string) (string, bool, error) {
	digest := sha256.Sum256(data)
	name := "image-" + hex.EncodeToString(digest[:])[:nameDigestLength]

	for attempt := range maxNameAttempts {
		candidate := name
		if attempt > 0 {
			candidate += "-" + strconv.Itoa(attempt)
		}

		path := filepath.Join(directory, candidate+extension)

		contents, err := os.ReadFile(path) //nolint:gosec // a path built from a digest under the drops directory
		if errors.Is(err, fs.ErrNotExist) {
			return path, false, nil
		}
		if err != nil {
			return "", false, fmt.Errorf("inspect the image file: %w", err)
		}

		if bytes.Equal(contents, data) {
			return path, true, nil
		}
	}

	return "", false, errors.New("too many images share a name in the drops directory")
}
