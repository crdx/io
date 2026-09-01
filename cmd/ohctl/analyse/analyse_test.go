package analyse

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"crdx.org/io/session"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestCacheReportsAreReadFromEverySupportedWireShape(t *testing.T) {
	transcript := strings.Join([]string{
		"# HTTP transcript",
		"# provider: mixed",
		`data: {"type":"response.completed","padding":"` + strings.Repeat("x", 10_000) +
			`","response":{"usage":{"input_tokens":2600,"input_tokens_details":{"cached_tokens":2000,"cache_write_tokens":400}}}}`,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":50,"cache_read_input_tokens":100000,"cache_creation_input_tokens":248}}}`,
		`data: {"usage":{"prompt_tokens":2006,"prompt_tokens_details":{"cached_tokens":1920}}}`,
		`data: {"type":"message_delta","usage":{"input_tokens":50,"cache_read_input_tokens":100000,"cache_creation_input_tokens":0}}`,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":900}}}`,
	}, "\n")

	provider, reports, err := readTranscript(strings.NewReader(transcript))
	if err != nil {
		t.Fatal(err)
	}
	if provider != "mixed" {
		t.Errorf("got provider %q, want mixed", provider)
	}
	want := []cacheReport{
		{inputTokens: 2600, cachedTokens: 2000, writtenTokens: 400},
		{inputTokens: 100298, cachedTokens: 100000, writtenTokens: 248},
		{inputTokens: 2006, cachedTokens: 1920},
	}
	if !reflect.DeepEqual(reports, want) {
		t.Errorf("got reports %#v, want %#v", reports, want)
	}
}

func TestZeroCachedTokensAreAMiss(t *testing.T) {
	statistics := CacheStatistics{}
	statistics.record(cacheReport{inputTokens: 9000})
	statistics.record(cacheReport{inputTokens: 12000, cachedTokens: 8000})

	if statistics.Requests != 2 || statistics.Hits != 1 || statistics.Misses != 1 {
		t.Errorf("got requests=%d hits=%d misses=%d", statistics.Requests, statistics.Hits, statistics.Misses)
	}
}

func TestSessionsWithoutWireUsageDoNotAffectTheAnalysis(t *testing.T) {
	directory := t.TempDir()
	writeTranscript(t, directory, "with-usage", strings.Join([]string{
		"# provider: codex",
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":9000,"input_tokens_details":{"cached_tokens":0}}}}`,
	}, "\n"))
	writeJournal(t, directory, "without-usage", "codex")

	analysis, err := analyseSessions(directory, []string{"with-usage", "without-usage"})
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.PromptCache.Providers) != 1 {
		t.Fatalf("got %d providers, want 1", len(analysis.PromptCache.Providers))
	}
	statistics := analysis.PromptCache.Providers[0]
	if statistics.Sessions != 1 || statistics.Misses != 1 {
		t.Errorf("got sessions=%d misses=%d, want 1 and 1", statistics.Sessions, statistics.Misses)
	}
}

func TestHistoricalAnalysisCacheIsReusedAndInvalidated(t *testing.T) {
	directory := t.TempDir()
	name := "cached-wire"
	firstReport := strings.Join([]string{
		"# provider: codex",
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":9000,"input_tokens_details":{"cached_tokens":8000}}}}`,
	}, "\n")
	writeTranscript(t, directory, name, firstReport)
	cachePath := filepath.Join(t.TempDir(), "analysis.json")

	first, err := analyseSessionsWithCache(directory, cachePath, []string{name})
	if err != nil {
		t.Fatal(err)
	}
	readCache := readAnalysisCache(cachePath)
	if len(readCache.Sessions) != 1 || first.PromptCache.Total.Requests != 1 {
		t.Fatalf("cache or first analysis was incomplete: %#v %#v", readCache, first)
	}

	secondReport := `data: {"type":"response.completed","response":{"usage":{"input_tokens":10000,"input_tokens_details":{"cached_tokens":0}}}}`
	writeTranscript(t, directory, name, firstReport+"\n"+secondReport)
	second, err := analyseSessionsWithCache(directory, cachePath, []string{name})
	if err != nil {
		t.Fatal(err)
	}
	if second.PromptCache.Total.Requests != 2 || second.PromptCache.Total.Misses != 1 {
		t.Errorf("changed wire transcript did not invalidate the cache: %#v", second)
	}
}

func TestCompleteJournalUsageAvoidsTheWireTranscript(t *testing.T) {
	directory := t.TempDir()
	writeTranscript(t, directory, "journal-usage", strings.Join([]string{
		"# provider: codex",
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":9000,"input_tokens_details":{"cached_tokens":0}}}}`,
	}, "\n"))
	writeJournal(
		t,
		directory,
		"journal-usage",
		"codex",
		`{"kind":"event","time":"2026-09-01T00:00:01Z","event":{"kind":"model_message","text":"hello","usage":{"input_tokens":9000,"cache":{"read_tokens":8000}}}}`,
	)

	statistics, hasStatistics, err := analyseSession(directory, "journal-usage")
	if err != nil {
		t.Fatal(err)
	}
	if !hasStatistics || statistics.Hits != 1 || statistics.Misses != 0 {
		t.Errorf("got statistics %#v", statistics)
	}
}

func TestPromptCacheAnalysisMatchesTheGolden(t *testing.T) {
	analysis := Analysis{PromptCache: PromptCacheAnalysis{
		Providers: []CacheStatistics{
			{
				Provider:        "anthropic",
				Sessions:        2,
				Requests:        8,
				Hits:            6,
				Misses:          2,
				InputTokens:     500000,
				CachedTokens:    450000,
				WrittenTokens:   25000,
				PeakInputTokens: 100000,
			},
			{
				Provider:        "codex",
				Sessions:        3,
				Requests:        10,
				Hits:            7,
				Misses:          3,
				InputTokens:     250000,
				CachedTokens:    175000,
				WrittenTokens:   10000,
				PeakInputTokens: 40000,
			},
		},
		Total: CacheStatistics{
			Sessions:        5,
			Requests:        18,
			Hits:            13,
			Misses:          5,
			InputTokens:     750000,
			CachedTokens:    625000,
			WrittenTokens:   35000,
			PeakInputTokens: 100000,
		},
	}}

	var output bytes.Buffer
	if err := writeText(analysis, &output); err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "prompt-cache.txt", output.String())
}

func TestJSONKeepsAnalysisSectionsExpandable(t *testing.T) {
	analysis := Analysis{PromptCache: PromptCacheAnalysis{
		Providers: []CacheStatistics{{Provider: "codex", Requests: 2, Hits: 1, Misses: 1}},
		Total:     CacheStatistics{Requests: 2, Hits: 1, Misses: 1},
	}}

	var output bytes.Buffer
	if err := writeJSON(analysis, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"promptCache"`) || !strings.Contains(output.String(), `"providers"`) {
		t.Errorf("unexpected JSON: %s", output.String())
	}
}

func writeTranscript(t *testing.T, directory string, name string, content string) {
	t.Helper()
	writeJournal(t, directory, name, "codex")
	path := filepath.Join(directory, name, wireTranscriptName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeJournal(t *testing.T, directory string, name string, provider string, events ...string) {
	t.Helper()
	path := filepath.Join(directory, name, "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	head := fmt.Sprintf(
		`{"kind":"head","time":"2026-09-01T00:00:00Z","version":%d,"name":%q,"meta":{"provider":%q}}`,
		session.JournalFormat,
		name,
		provider,
	)
	content := strings.Join(append([]string{head}, events...), "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertGolden(t *testing.T, name string, drawn string) {
	t.Helper()
	root, err := os.OpenRoot("testdata")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	if *updateGoldens {
		if err := root.WriteFile(name, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := root.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if drawn != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", name, drawn, want)
	}
}
