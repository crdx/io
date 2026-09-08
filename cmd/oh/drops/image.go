package drops

import (
	"fmt"
	"os"
)

var imageExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

func SaveImage(sessionDirectory string, ensureSession func() error, mediaType string, data []byte) (string, error) {
	extension, isKnown := imageExtensions[mediaType]
	if !isKnown {
		return "", fmt.Errorf("the clipboard holds an unsupported image type %q", mediaType)
	}

	directory, err := Prepare(sessionDirectory, ensureSession)
	if err != nil {
		return "", err
	}

	file, err := os.CreateTemp(directory, "image-*"+extension)
	if err != nil {
		return "", fmt.Errorf("create the image file: %w", err)
	}
	path := file.Name()

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)

		return "", fmt.Errorf("write the image file: %w", err)
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(path)

		return "", fmt.Errorf("close the image file: %w", err)
	}

	return path, nil
}
