package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const shortestFence = 3

var transcriptParser = goldmark.New().Parser()

func TestEveryTranscriptIsWellFormedMarkdown(t *testing.T) {
	for _, path := range everyTranscriptGolden(t) {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".transcript"), func(t *testing.T) {
			source := readTranscriptGolden(t, path)

			reportListItemsSpanningSeveralLines(t, source)
			reportBlocksTouchingTheOneBefore(t, source)
		})
	}
}

func everyTranscriptGolden(t *testing.T) []string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("testdata", "output", "*.transcript"))
	if err != nil {
		t.Fatal(err)
	}

	if len(paths) == 0 {
		t.Fatal("expected the transcript goldens to be found")
	}

	return paths
}

func readTranscriptGolden(t *testing.T, path string) []byte {
	t.Helper()

	golden, err := os.ReadFile(path) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}

	var transcript []string

	for line := range strings.SplitSeq(string(golden), "\n") {
		if !strings.HasPrefix(line, "=== ") {
			transcript = append(transcript, line)
		}
	}

	return []byte(strings.Join(transcript, "\n"))
}

func reportListItemsSpanningSeveralLines(t *testing.T, source []byte) {
	t.Helper()

	document := transcriptParser.Parse(text.NewReader(source))

	err := ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || node.Kind() != ast.KindListItem {
			return ast.WalkContinue, nil
		}

		if item := itemText(node, source); strings.Contains(strings.TrimSuffix(item, "\n"), "\n") {
			t.Errorf("a field swallowed the block below it:\n%s", item)
		}

		return ast.WalkContinue, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func itemText(node ast.Node, source []byte) string {
	var item strings.Builder

	writeLines(&item, node, source)

	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		writeLines(&item, child, source)
	}

	return item.String()
}

func writeLines(item *strings.Builder, node ast.Node, source []byte) {
	lines := node.Lines()

	for line := range lines.Len() {
		segment := lines.At(line)
		item.Write(segment.Value(source))
	}
}

func reportBlocksTouchingTheOneBefore(t *testing.T, source []byte) {
	t.Helper()

	previous := ""
	openFence := ""

	for line := range strings.SplitSeq(string(source), "\n") {
		switch {
		case openFence != "":
			if backtickRun(line) >= len(openFence) && strings.TrimRight(line, "`") == "" {
				openFence = ""
			}
		case backtickRun(line) >= shortestFence:
			openFence = line[:backtickRun(line)]

			if previous != "" {
				t.Errorf("a fence opened straight after %q", previous)
			}
		case previous != "" && startsABlock(line, previous):
			t.Errorf("%q started straight after %q", line, previous)
		}

		previous = line
	}
}

func backtickRun(line string) int {
	return len(line) - len(strings.TrimLeft(line, "`"))
}

func startsABlock(line string, previous string) bool {
	switch {
	case strings.HasPrefix(line, "#"), strings.HasPrefix(line, ">"), strings.HasPrefix(line, "**"):
		return true
	case strings.HasPrefix(line, "- "):
		return !strings.HasPrefix(previous, "- ")
	default:
		return false
	}
}
