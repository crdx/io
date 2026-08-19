package read_test

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/file"
	"crdx.org/io/tool"
	"crdx.org/io/toolbox/read"
)

func testRoot(t *testing.T, name string, content string) *file.Root {
	t.Helper()

	directory := t.TempDir()

	if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return file.New(root, allowAll)
}

func exec(t *testing.T, root *file.Root, arguments string) (string, error) {
	t.Helper()

	call, err := read.New(root, file.NewSnapshots()).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := call.Exec(t.Context())
	return result.Output, err
}

func TestAnImageIsAttachedForTheModel(t *testing.T) {
	content := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 24)
	root := testRoot(t, "picture.png", content)
	call, err := read.New(root, file.NewSnapshots()).Parse(`{"path":"picture.png"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "image/png image (32 bytes)" {
		t.Errorf("expected an image description, got %q", result.Output)
	}

	if result.Image.MediaType != "image/png" || string(result.Image.Data) != content {
		t.Errorf("expected the PNG bytes, got %q and %d bytes", result.Image.MediaType, len(result.Image.Data))
	}
}

func TestAnImageReportsAnEstimateFromItsDimensions(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 64, 33))); err != nil {
		t.Fatalf("could not encode the test image: %v", err)
	}

	root := testRoot(t, "picture.png", encoded.String())
	call, err := read.New(root, file.NewSnapshots()).Parse(`{"path":"picture.png"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Stats.Kind != tool.StatsImage || result.Stats.EstimatedTokens != 4 {
		t.Errorf("expected a four-token image estimate, got %#v", result.Stats)
	}
}

func TestAnImageCannotBeReadAsLines(t *testing.T) {
	content := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 24)
	root := testRoot(t, "picture.png", content)

	_, err := exec(t, root, `{"path":"picture.png","limit":1}`)
	if err == nil || err.Error() != "line ranges are not supported for images" {
		t.Errorf("expected a line range to be refused, got %v", err)
	}
}

func TestAFileWithNoRangeComesBackWhole(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\ntwo\nthree\n")

	output, err := exec(t, root, `{"path":"notes.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "one\ntwo\nthree\n" {
		t.Errorf("expected the whole file, got %q", output)
	}
}

func TestALineRangeComesBackOnItsOwn(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\ntwo\nthree\nfour\n")

	output, err := exec(t, root, `{"path":"notes.txt","offset":2,"limit":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "two\nthree" {
		t.Errorf("expected the second and third lines, got %q", output)
	}
}

func TestALineRangeMeasuresOnlyWhatComesBack(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\ntwo\nthree\nfour\n")
	call, err := read.New(root, file.NewSnapshots()).Parse(`{"path":"notes.txt","offset":2,"limit":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Stats.Lines != 2 || result.Stats.Bytes != int64(len(result.Output)) {
		t.Errorf(
			"expected 2 lines and %d bytes, got %d lines and %d bytes",
			len(result.Output), result.Stats.Lines, result.Stats.Bytes,
		)
	}
}

func TestALineRangeOfAnEmptyFileIsEmpty(t *testing.T) {
	root := testRoot(t, "empty.txt", "")

	output, err := exec(t, root, `{"path":"empty.txt","offset":1,"limit":100}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != "" {
		t.Errorf("expected empty output, got %q", output)
	}
}

func TestAnOffsetPastTheEndSaysHowLongTheFileIs(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\ntwo\n")

	_, err := exec(t, root, `{"path":"notes.txt","offset":9}`)
	if err == nil {
		t.Fatal("expected an offset past the end to be refused")
	}

	if !strings.Contains(err.Error(), "2 lines") {
		t.Errorf("expected the length to be named, got %q", err)
	}
}

func TestAHugeFileIsNotCutShort(t *testing.T) {
	content := strings.Repeat("a line of text\n", 8000)
	root := testRoot(t, "big.txt", content)

	output, err := exec(t, root, `{"path":"big.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != content {
		t.Errorf("expected %d bytes, got %d", len(content), len(output))
	}
}

func TestAMissingFileDoesNotExposeHowItWasOpened(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\n")

	_, err := exec(t, root, `{"path":"missing.txt"}`)
	if err == nil {
		t.Fatal("expected a missing file to fail")
	}

	const expected = "missing.txt: no such file or directory"

	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err)
	}
}

func TestAPathOutsideTheRootIsRefused(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\n")

	if _, err := exec(t, root, `{"path":"../../etc/passwd"}`); err == nil {
		t.Error("expected a path outside the root to be refused")
	}
}

func TestAFileInAMountedRootCanBeRead(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\n")
	scratch := testRoot(t, "result.txt", "answer\n")
	root.Mount("/tmp", scratch)

	output, err := exec(t, root, `{"path":"/tmp/result.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != "answer\n" {
		t.Errorf("got %q, want %q", output, "answer\n")
	}
}

func TestAReadWithNoPathIsRefused(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\n")

	if _, err := exec(t, root, `{}`); err == nil {
		t.Error("expected a read with no path to be refused")
	}
}

func TestAReadFocusesTheFileName(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\n")
	call, err := read.New(root, file.NewSnapshots()).Parse(`{"path":"somewhere/notes.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := tool.Highlight{Kind: tool.HighlightFocus, Value: "notes.txt"}
	if call.Highlight() != want {
		t.Errorf("expected the file name to be focused, got %T", call)
	}
}

func TestRenderSaysWhichLinesAreBeingRead(t *testing.T) {
	renderedPath, detail := read.Render(read.Args{Path: "notes.txt", Offset: 10, Limit: 5})

	if renderedPath != "notes.txt" {
		t.Errorf("expected the path, got %q", renderedPath)
	}

	if detail != "10-14" {
		t.Errorf("expected the range, got %q", detail)
	}
}

func TestRenderWritesAPathBelowHomeWithATilde(t *testing.T) {
	t.Setenv("HOME", "/home/alice")

	renderedPath, _ := read.Render(read.Args{Path: "/home/alice/.agents/skills/golang/SKILL.md"})

	if want := "~/.agents/skills/golang/SKILL.md"; renderedPath != want {
		t.Errorf("got %q, want %q", renderedPath, want)
	}
}

func TestRenderLeavesAnOpenRangeOpen(t *testing.T) {
	_, detail := read.Render(read.Args{Path: "notes.txt", Offset: 10})

	if detail != "10+" {
		t.Errorf("expected an open range, got %q", detail)
	}
}

func TestRenderSaysNothingAboutAWholeFile(t *testing.T) {
	_, detail := read.Render(read.Args{Path: "notes.txt"})

	if detail != "" {
		t.Errorf("expected no range, got %q", detail)
	}
}

func allowAll(string) error { return nil }
