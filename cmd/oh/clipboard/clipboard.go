package clipboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"crdx.org/io/cmd/oh/drops"
)

const readTimeout = 5 * time.Second

type imageFormat struct {
	mediaType string
	extension string
}

var imageFormats = []imageFormat{
	{mediaType: "image/png", extension: ".png"},
	{mediaType: "image/jpeg", extension: ".jpg"},
	{mediaType: "image/webp", extension: ".webp"},
	{mediaType: "image/gif", extension: ".gif"},
}

type sourceKind int

const (
	waylandSource sourceKind = iota
	xSource
)

type source struct {
	kind sourceKind
	name string
}

func SaveImage(sessionDirectory string, ensureSession func() error) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	availableSources := sources()
	if len(availableSources) == 0 {
		return "", errors.New("clipboard image paste needs wl-paste on Wayland or xclip on X11")
	}

	var failures []error
	for _, availableSource := range availableSources {
		mediaTypes, err := listMediaTypes(ctx, availableSource)
		if err != nil {
			failures = append(failures, err)
			continue
		}

		imageFormat, isFound := findImageFormat(mediaTypes)
		if !isFound {
			continue
		}

		if err := ensureSession(); err != nil {
			return "", fmt.Errorf("prepare the session directory: %w", err)
		}
		directory := drops.GetDirectory(sessionDirectory)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return "", fmt.Errorf("prepare the image directory: %w", err)
		}

		path, err := save(ctx, availableSource, imageFormat, directory)
		if err == nil {
			return path, nil
		}
		failures = append(failures, err)
	}

	if len(failures) > 0 {
		return "", errors.Join(failures...)
	}

	return "", errors.New("the clipboard does not contain a PNG, JPEG, WebP, or GIF image")
}

func sources() []source {
	var availableSources []source
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if _, err := exec.LookPath("wl-paste"); err == nil {
			availableSources = append(availableSources, source{kind: waylandSource, name: "wl-paste"})
		}
	}
	if os.Getenv("DISPLAY") != "" {
		if _, err := exec.LookPath("xclip"); err == nil {
			availableSources = append(availableSources, source{kind: xSource, name: "xclip"})
		}
	}
	return availableSources
}

func (self source) getListCommand(ctx context.Context) *exec.Cmd {
	if self.kind == waylandSource {
		return exec.CommandContext(ctx, "wl-paste", "--list-types")
	}
	return exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-target", "TARGETS", "-out")
}

func (self source) getReadCommand(ctx context.Context, mediaType string) *exec.Cmd {
	if self.kind == waylandSource {
		return getWaylandReadCommand(ctx, mediaType)
	}
	return getXReadCommand(ctx, mediaType)
}

func getWaylandReadCommand(ctx context.Context, mediaType string) *exec.Cmd {
	switch mediaType {
	case "image/png":
		return exec.CommandContext(ctx, "wl-paste", "--no-newline", "--type", "image/png")
	case "image/jpeg":
		return exec.CommandContext(ctx, "wl-paste", "--no-newline", "--type", "image/jpeg")
	case "image/webp":
		return exec.CommandContext(ctx, "wl-paste", "--no-newline", "--type", "image/webp")
	case "image/gif":
		return exec.CommandContext(ctx, "wl-paste", "--no-newline", "--type", "image/gif")
	default:
		panic("unsupported clipboard image type")
	}
}

func getXReadCommand(ctx context.Context, mediaType string) *exec.Cmd {
	switch mediaType {
	case "image/png":
		return exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-target", "image/png", "-out")
	case "image/jpeg":
		return exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-target", "image/jpeg", "-out")
	case "image/webp":
		return exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-target", "image/webp", "-out")
	case "image/gif":
		return exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-target", "image/gif", "-out")
	default:
		panic("unsupported clipboard image type")
	}
}

func listMediaTypes(ctx context.Context, availableSource source) ([]string, error) {
	command := availableSource.getListCommand(ctx)
	output, err := command.Output()
	if err != nil {
		return nil, commandError(availableSource.name, err)
	}

	return strings.Fields(strings.ToLower(string(output))), nil
}

func findImageFormat(mediaTypes []string) (imageFormat, bool) {
	for _, imageFormat := range imageFormats {
		if slices.Contains(mediaTypes, imageFormat.mediaType) {
			return imageFormat, true
		}
	}
	return imageFormat{}, false
}

func save(ctx context.Context, availableSource source, imageFormat imageFormat, directory string) (string, error) {
	file, err := os.CreateTemp(directory, "image-*"+imageFormat.extension)
	if err != nil {
		return "", fmt.Errorf("create the image file: %w", err)
	}
	path := file.Name()

	var standardError bytes.Buffer
	command := availableSource.getReadCommand(ctx, imageFormat.mediaType)
	command.Stdout = file
	command.Stderr = &standardError
	if err := command.Run(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", commandErrorWithOutput(availableSource.name, err, standardError.String())
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close the image file: %w", err)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("inspect the image file: %w", err)
	}
	if fileInfo.Size() == 0 {
		_ = os.Remove(path)
		return "", errors.New("the clipboard returned an empty image")
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("resolve the image path: %w", err)
	}
	return absolutePath, nil
}

func commandError(executable string, err error) error {
	if exitError, isFound := errors.AsType[*exec.ExitError](err); isFound {
		return commandErrorWithOutput(executable, err, string(exitError.Stderr))
	}
	return fmt.Errorf("%s: %w", executable, err)
}

func commandErrorWithOutput(executable string, err error, output string) error {
	detail := strings.TrimSpace(output)
	if detail != "" {
		return fmt.Errorf("%s: %s", executable, detail)
	}
	return fmt.Errorf("%s: %w", executable, err)
}
