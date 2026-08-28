package modelPicker

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/picker"
	"crdx.org/io/internal/util/strutil"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func compareWithGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)

	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}

	if drawn != string(want) {
		t.Errorf("rows differ from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}

func availableModels() []*Model {
	return []*Model{
		{
			Provider:            "Anthropic",
			ProviderID:          "anthropic",
			Name:                "Sonnet 5",
			ID:                  "claude-sonnet-5",
			EffortLevels:        []string{"none", "low", "medium", "high"},
			Effort:              "medium",
			ContextWindowTokens: 200000,
		},
		{
			Provider:            "Codex",
			ProviderID:          "codex",
			Name:                "Codex 5.3",
			ID:                  "gpt-5.3-codex",
			EffortLevels:        []string{"low", "medium", "high", "xhigh"},
			Effort:              "medium",
			ContextWindowTokens: 272000,
		},
		{
			Provider:            "OpenCode Go",
			ProviderID:          "opencode-go",
			Name:                "DeepSeek Flash Vision Exp 4",
			ID:                  "deepseek-v4-flash-vision-exp",
			EffortLevels:        []string{"low", "medium", "high"},
			Effort:              "high",
			ContextWindowTokens: 1000000,
		},
		{
			Provider:            "Ollama",
			ProviderID:          "ollama",
			Name:                "Qwen Coder 3 30B",
			ID:                  "qwen3-coder:30b",
			EffortLevels:        []string{"medium"},
			Effort:              "medium",
			ContextWindowTokens: 0,
		},
	}
}

func TestEveryModelCanBeChosen(t *testing.T) {
	models := &modelList{models: availableModels()}

	for index := range models.Len() {
		if !models.IsChoosable(index) {
			t.Errorf("expected model %d to be available, got otherwise", index)
		}
	}
}

func TestTheEffortOfAModelIsSetOneLevelAtATime(t *testing.T) {
	models := availableModels()
	list := &modelList{models: models}

	list.Adjust(1, 1)
	if models[1].Effort != "high" {
		t.Errorf("expected a higher effort, got %q", models[1].Effort)
	}

	for range 5 {
		list.Adjust(1, 1)
	}
	if models[1].Effort != "xhigh" {
		t.Errorf("expected the effort to stop at the highest, got %q", models[1].Effort)
	}

	for range 5 {
		list.Adjust(1, -1)
	}
	if models[1].Effort != "low" {
		t.Errorf("expected the effort to stop at the lowest, got %q", models[1].Effort)
	}

	if models[0].Effort != "medium" {
		t.Errorf("expected the other models to keep their effort, got %q", models[0].Effort)
	}
}

func TestAnEffortTheModelDoesNotOfferIsLeftAlone(t *testing.T) {
	models := []*Model{{EffortLevels: []string{"low", "high"}, Effort: "medium"}}
	(&modelList{models: models}).Adjust(0, 1)

	if models[0].Effort != "medium" {
		t.Errorf("expected the effort to be left alone, got %q", models[0].Effort)
	}
}

func TestAContextWindowIsWrittenTheWayEveryTokenCountIs(t *testing.T) {
	cases := map[int]string{
		0:       "—",
		512:     "1K",
		64000:   "64K",
		272000:  "272K",
		1048576: "1M",
	}

	for count, want := range cases {
		if got := contextWindow(count); got != want {
			t.Errorf("contextWindow(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestTheRowsOfTheModelPickerMatchTheGolden(t *testing.T) {
	models := &modelList{models: availableModels()}

	var output strings.Builder

	for i, room := range []int{150, 80, 46} {
		if i > 0 {
			_, _ = fmt.Fprintln(&output)
		}
		_, _ = fmt.Fprintf(&output, "--- %d columns ---\n", room)
		_, _ = fmt.Fprintln(&output, models.ColumnHeader(room))
		for index, model := range models.models {
			described, identifier := modelRow(model, index == 1, room)
			_, _ = fmt.Fprintln(&output, described+identifier)
		}
	}

	compareWithGolden(t, "rows.golden", output.String())
}

func TestWhatTheModelPickerPaintsMatchesTheGolden(t *testing.T) {
	frames := []struct {
		name   string
		room   int
		height int
		cursor int
		query  string
	}{
		{name: "a wide terminal, where the columns stay to the left", room: 150, height: 24, cursor: 0},
		{name: "a terminal the columns fill exactly", room: 80, height: 24, cursor: 1},
		{name: "no room for every row, so the list is scrolled to the cursor", room: 80, height: 3, cursor: 2},
		{name: "a narrow terminal, where the columns are clipped", room: 46, height: 24, cursor: 0},
		{name: "a filter narrowing the list to one provider", room: 80, height: 24, cursor: 0, query: "opencode"},
	}

	var output strings.Builder

	for _, frame := range frames {
		fmt.Fprintf(&output, "=== %s ===\n%s\n", frame.name, strutil.VisibleEscapes(
			picker.Paint(
				&modelList{models: availableModels()},
				frame.room,
				frame.height,
				frame.cursor,
				frame.query,
			),
		))
	}

	compareWithGolden(t, "painted.ansi", output.String())
}
