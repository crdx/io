package analyse

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"crdx.org/duckopt/v2"

	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/ohctl/console"
	"crdx.org/io/internal/util"
	"crdx.org/io/session"
)

const usage = `ohctl analyse — analyse stored sessions

Usage:
    $0 analyse [options] [<session>...]

Options:
    -j, --json    Write the analysis as JSON
    -h, --help    Show this help

Sessions are named on the command line, or every stored session is analysed when none is.
`

const (
	wireTranscriptName    = "wire.http"
	journalTranscriptName = "session.jsonl"
	providerPrefix        = "# provider: "
	dataPrefix            = "data: "
)

type inputOpts struct {
	Analyse  bool     `docopt:"analyse"`
	JSON     bool     `docopt:"--json"`
	Sessions []string `docopt:"<session>"`
}

type Analysis struct {
	PromptCache PromptCacheAnalysis `json:"promptCache"`
}

type PromptCacheAnalysis struct {
	Providers []CacheStatistics `json:"providers"`
	Total     CacheStatistics   `json:"total"`
}

type CacheStatistics struct {
	Provider        string `json:"provider,omitempty"`
	Sessions        int    `json:"sessions"`
	Requests        int    `json:"requests"`
	Hits            int    `json:"hits"`
	Misses          int    `json:"misses"`
	InputTokens     int64  `json:"inputTokens"`
	CachedTokens    int64  `json:"cachedTokens"`
	WrittenTokens   int64  `json:"writtenTokens"`
	PeakInputTokens int64  `json:"peakInputTokens"`
}

const analysisCacheFormat = 2

type analysisCache struct {
	Format   int                      `json:"format"`
	Sessions map[string]cachedSession `json:"sessions"`
}

type cachedSession struct {
	JournalSize       int64           `json:"journalSize"`
	JournalModifiedAt int64           `json:"journalModifiedAt"`
	WireSize          int64           `json:"wireSize"`
	WireModifiedAt    int64           `json:"wireModifiedAt"`
	Statistics        CacheStatistics `json:"statistics"`
	HasStatistics     bool            `json:"hasStatistics"`
}

type wireLineKind int

const (
	unknownWireLine wireLineKind = iota
	responsesWireLine
	anthropicWireLine
	chatCompletionsWireLine
)

type wireLine struct {
	kind             wireLineKind
	inputTokens      int64
	cachedTokens     int64
	writtenTokens    int64
	hasInputTokens   bool
	hasCachedTokens  bool
	hasWrittenTokens bool
}

type cacheReport struct {
	inputTokens   int64
	cachedTokens  int64
	writtenTokens int64
}

func Run() error {
	options := duckopt.MustBind[inputOpts](usage, "$0")
	return run(
		location.GetSessionsDir(),
		location.GetAnalysisCachePath(),
		options.Sessions,
		options.JSON,
		console.Standard(),
	)
}

func run(directory string, cachePath string, names []string, isJSON bool, output console.Output) error {
	selectedNames, err := selectNames(directory, names)
	if err != nil {
		return err
	}

	analysis, err := analyseSessionsWithCache(directory, cachePath, selectedNames)
	if err != nil {
		return err
	}

	if isJSON {
		return writeJSON(analysis, output.Screen)
	}

	return writeText(analysis, output.Screen)
}

func selectNames(directory string, requestedNames []string) ([]string, error) {
	entries, err := session.Entries(directory)
	if err != nil {
		return nil, err
	}

	available := make(map[string]bool, len(entries))
	for _, entry := range entries {
		available[entry.Name] = true
	}

	if len(requestedNames) == 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name)
		}
		return names, nil
	}

	for _, name := range requestedNames {
		if !available[name] {
			return nil, fmt.Errorf("there is no stored session named %q", name)
		}
	}

	return requestedNames, nil
}

type analysisJob struct {
	name             string
	cachedStatistics cachedSession
	hasCached        bool
}

type sessionAnalysis struct {
	name             string
	statistics       CacheStatistics
	cachedStatistics cachedSession
	err              error
	hasStatistics    bool
	hasCached        bool
}

func analyseSessions(directory string, names []string) (Analysis, error) {
	return analyseSessionsWithCache(directory, "", names)
}

func analyseSessionsWithCache(directory string, cachePath string, names []string) (Analysis, error) {
	cache := readAnalysisCache(cachePath)
	jobs := make(chan analysisJob, len(names))
	results := make(chan sessionAnalysis)
	var workers sync.WaitGroup

	for _, name := range names {
		cachedStatistics, hasCached := cache.Sessions[name]
		jobs <- analysisJob{name: name, cachedStatistics: cachedStatistics, hasCached: hasCached}
	}
	close(jobs)

	for range min(runtime.GOMAXPROCS(0), len(names)) {
		workers.Go(func() {
			for job := range jobs {
				result := analyseSessionWithCache(directory, job.name, job.cachedStatistics, job.hasCached)
				result.name = job.name
				results <- result
			}
		})
	}
	go func() {
		workers.Wait()
		close(results)
	}()

	byProvider := map[string]*CacheStatistics{}
	var firstError error
	for result := range results {
		if result.err != nil {
			if firstError == nil {
				firstError = result.err
			}
			continue
		}
		if result.hasCached {
			cache.Sessions[result.name] = result.cachedStatistics
		}
		if !result.hasStatistics {
			continue
		}
		statistics := byProvider[result.statistics.Provider]
		if statistics == nil {
			statistics = &CacheStatistics{Provider: result.statistics.Provider}
			byProvider[result.statistics.Provider] = statistics
		}
		statistics.add(result.statistics)
	}
	if firstError != nil {
		return Analysis{}, firstError
	}
	_ = writeAnalysisCache(cachePath, cache)

	providers := make([]CacheStatistics, 0, len(byProvider))
	for _, statistics := range byProvider {
		providers = append(providers, *statistics)
	}
	slices.SortFunc(providers, func(first CacheStatistics, second CacheStatistics) int {
		return strings.Compare(first.Provider, second.Provider)
	})

	analysis := Analysis{PromptCache: PromptCacheAnalysis{Providers: providers}}
	for _, statistics := range providers {
		analysis.PromptCache.Total.add(statistics)
	}
	return analysis, nil
}

func analyseSession(directory string, name string) (CacheStatistics, bool, error) {
	result := analyseSessionWithCache(directory, name, cachedSession{}, false)
	return result.statistics, result.hasStatistics, result.err
}

func analyseSessionWithCache(
	directory string,
	name string,
	cachedStatistics cachedSession,
	hasCached bool,
) sessionAnalysis {
	sessionRoot, err := os.OpenRoot(session.Dir(directory, name))
	if err != nil {
		return sessionAnalysis{err: fmt.Errorf("could not read %s: %w", name, err)}
	}
	journalInfo, err := sessionRoot.Stat(journalTranscriptName)
	if err != nil {
		_ = sessionRoot.Close()
		return sessionAnalysis{err: fmt.Errorf("could not read %s: %w", name, err)}
	}
	wireSize := int64(-1)
	wireModifiedAt := int64(0)
	wireInfo, err := sessionRoot.Stat(wireTranscriptName)
	if err == nil {
		wireSize = wireInfo.Size()
		wireModifiedAt = wireInfo.ModTime().UnixNano()
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = sessionRoot.Close()
		return sessionAnalysis{err: fmt.Errorf("could not read %s: %w", name, err)}
	}

	if hasCached && cachedStatistics.matches(
		journalInfo.Size(),
		journalInfo.ModTime().UnixNano(),
		wireSize,
		wireModifiedAt,
	) {
		_ = sessionRoot.Close()
		return sessionAnalysis{
			statistics:    cachedStatistics.Statistics,
			hasStatistics: cachedStatistics.HasStatistics,
		}
	}

	journalStatistics, isComplete, err := analyseJournal(directory, name)
	if err != nil {
		_ = sessionRoot.Close()
		return sessionAnalysis{err: err}
	}
	if isComplete {
		_ = sessionRoot.Close()
		return cachedAnalysis(
			journalStatistics,
			true,
			journalInfo.Size(),
			journalInfo.ModTime().UnixNano(),
			wireSize,
			wireModifiedAt,
		)
	}
	if wireSize < 0 {
		_ = sessionRoot.Close()
		return cachedAnalysis(
			CacheStatistics{},
			false,
			journalInfo.Size(),
			journalInfo.ModTime().UnixNano(),
			wireSize,
			wireModifiedAt,
		)
	}

	file, err := sessionRoot.Open(wireTranscriptName)
	if err != nil {
		_ = sessionRoot.Close()
		return sessionAnalysis{err: fmt.Errorf("could not read %s: %w", name, err)}
	}
	provider, reports, readErr := readTranscript(file)
	closeErr := errors.Join(file.Close(), sessionRoot.Close())
	if err := errors.Join(readErr, closeErr); err != nil {
		return sessionAnalysis{err: fmt.Errorf("could not read %s: %w", name, err)}
	}

	statistics := CacheStatistics{Provider: provider, Sessions: 1}
	if statistics.Provider == "" {
		statistics.Provider = "unknown"
	}
	for _, report := range reports {
		statistics.record(report)
	}
	return cachedAnalysis(
		statistics,
		len(reports) > 0,
		journalInfo.Size(),
		journalInfo.ModTime().UnixNano(),
		wireSize,
		wireModifiedAt,
	)
}

func (self cachedSession) matches(
	journalSize int64,
	journalModifiedAt int64,
	wireSize int64,
	wireModifiedAt int64,
) bool {
	return self.JournalSize == journalSize &&
		self.JournalModifiedAt == journalModifiedAt &&
		self.WireSize == wireSize &&
		self.WireModifiedAt == wireModifiedAt
}

func cachedAnalysis(
	statistics CacheStatistics,
	hasStatistics bool,
	journalSize int64,
	journalModifiedAt int64,
	wireSize int64,
	wireModifiedAt int64,
) sessionAnalysis {
	return sessionAnalysis{
		statistics:    statistics,
		hasStatistics: hasStatistics,
		cachedStatistics: cachedSession{
			JournalSize:       journalSize,
			JournalModifiedAt: journalModifiedAt,
			WireSize:          wireSize,
			WireModifiedAt:    wireModifiedAt,
			Statistics:        statistics,
			HasStatistics:     hasStatistics,
		},
		hasCached: true,
	}
}

func analyseJournal(directory string, name string) (CacheStatistics, bool, error) {
	statistics := CacheStatistics{Sessions: 1}
	usageReports := 0

	err := session.Records(directory, name, func(line session.Line) error {
		if line.Kind == session.Head {
			var meta struct {
				Provider string `json:"provider"`
			}
			if err := json.Unmarshal(line.Meta, &meta); err != nil {
				return err
			}
			statistics.Provider = meta.Provider
			return nil
		}
		if line.Event == nil || line.Event.Usage == nil || line.Event.Usage.InputTokens <= 0 {
			return nil
		}

		usageReports++
		if line.Event.Usage.Cache != nil {
			statistics.record(cacheReport{
				inputTokens:   int64(line.Event.Usage.InputTokens),
				cachedTokens:  int64(line.Event.Usage.Cache.ReadTokens),
				writtenTokens: int64(line.Event.Usage.Cache.WriteTokens),
			})
		}
		return nil
	})
	if err != nil {
		return CacheStatistics{}, false, fmt.Errorf("could not analyse %s: %w", name, err)
	}
	if statistics.Provider == "" {
		statistics.Provider = "unknown"
	}
	return statistics, usageReports > 0 && statistics.Requests == usageReports, nil
}

func (self *CacheStatistics) record(report cacheReport) {
	self.Requests++
	self.InputTokens += report.inputTokens
	self.CachedTokens += report.cachedTokens
	self.WrittenTokens += report.writtenTokens
	self.PeakInputTokens = max(self.PeakInputTokens, report.inputTokens)
	if report.cachedTokens > 0 {
		self.Hits++
	} else {
		self.Misses++
	}
}

func (self *CacheStatistics) add(statisticsToAdd CacheStatistics) {
	self.Sessions += statisticsToAdd.Sessions
	self.Requests += statisticsToAdd.Requests
	self.Hits += statisticsToAdd.Hits
	self.Misses += statisticsToAdd.Misses
	self.InputTokens += statisticsToAdd.InputTokens
	self.CachedTokens += statisticsToAdd.CachedTokens
	self.WrittenTokens += statisticsToAdd.WrittenTokens
	self.PeakInputTokens = max(self.PeakInputTokens, statisticsToAdd.PeakInputTokens)
}

var (
	responsesCompletedType = []byte(`"type":"response.completed"`)
	responsesDoneType      = []byte(`"type":"response.done"`)
	anthropicStartType     = []byte(`"type":"message_start"`)
	inputTokensKey         = []byte(`"input_tokens":`)
	promptTokensKey        = []byte(`"prompt_tokens":`)
	cachedTokensKey        = []byte(`"cached_tokens":`)
	cacheReadTokensKey     = []byte(`"cache_read_input_tokens":`)
	cacheWriteTokensKey    = []byte(`"cache_write_tokens":`)
	cacheCreationTokensKey = []byte(`"cache_creation_input_tokens":`)
)

const fragmentOverlap = 96

func readTranscript(reader io.Reader) (string, []cacheReport, error) {
	bufferedReader := bufio.NewReader(reader)
	provider := ""
	var reports []cacheReport
	var line wireLine
	var overlap []byte
	isLineStart := true

	for {
		fragment, err := bufferedReader.ReadSlice('\n')
		if isLineStart {
			if providerName, isProvider := bytes.CutPrefix(fragment, []byte(providerPrefix)); isProvider {
				provider = strings.TrimSpace(string(providerName))
			}
			line.start(fragment)
		}
		if line.kind != unknownWireLine {
			line.inspect(fragment)
			if len(overlap) > 0 {
				boundary := append(slices.Clone(overlap), fragment[:min(len(fragment), fragmentOverlap)]...)
				line.inspect(boundary)
			}
			overlap = trailingBytes(overlap, fragment)
		}

		isLineEnd := !errors.Is(err, bufio.ErrBufferFull)
		if isLineEnd {
			if report, isReported := line.report(); isReported {
				reports = append(reports, report)
			}
			line = wireLine{}
			overlap = overlap[:0]
			isLineStart = true
		} else {
			isLineStart = false
		}

		if errors.Is(err, io.EOF) {
			return provider, reports, nil
		}
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			return "", nil, err
		}
	}
}

func (self *wireLine) start(fragment []byte) {
	if !bytes.HasPrefix(fragment, []byte(dataPrefix)) {
		return
	}

	switch {
	case bytes.Contains(fragment, responsesCompletedType), bytes.Contains(fragment, responsesDoneType):
		self.kind = responsesWireLine
	case bytes.Contains(fragment, anthropicStartType):
		self.kind = anthropicWireLine
	default:
		self.kind = chatCompletionsWireLine
	}
}

func (self *wireLine) inspect(fragment []byte) {
	switch self.kind {
	case responsesWireLine:
		self.inputTokens, self.hasInputTokens = readNumber(fragment, inputTokensKey, self.inputTokens, self.hasInputTokens)
		self.cachedTokens, self.hasCachedTokens = readNumber(fragment, cachedTokensKey, self.cachedTokens, self.hasCachedTokens)
		self.writtenTokens, self.hasWrittenTokens = readNumber(
			fragment,
			cacheWriteTokensKey,
			self.writtenTokens,
			self.hasWrittenTokens,
		)
	case anthropicWireLine:
		self.inputTokens, self.hasInputTokens = readNumber(fragment, inputTokensKey, self.inputTokens, self.hasInputTokens)
		self.cachedTokens, self.hasCachedTokens = readNumber(fragment, cacheReadTokensKey, self.cachedTokens, self.hasCachedTokens)
		self.writtenTokens, self.hasWrittenTokens = readNumber(
			fragment,
			cacheCreationTokensKey,
			self.writtenTokens,
			self.hasWrittenTokens,
		)
	case chatCompletionsWireLine:
		self.inputTokens, self.hasInputTokens = readNumber(fragment, promptTokensKey, self.inputTokens, self.hasInputTokens)
		self.cachedTokens, self.hasCachedTokens = readNumber(fragment, cachedTokensKey, self.cachedTokens, self.hasCachedTokens)
		self.writtenTokens, self.hasWrittenTokens = readNumber(
			fragment,
			cacheWriteTokensKey,
			self.writtenTokens,
			self.hasWrittenTokens,
		)
	case unknownWireLine:
	}
}

func (self *wireLine) report() (cacheReport, bool) {
	if !self.hasInputTokens || !self.hasCachedTokens {
		return cacheReport{}, false
	}
	if self.kind == anthropicWireLine {
		self.inputTokens += self.cachedTokens + self.writtenTokens
	}
	return cacheReport{
		inputTokens:   self.inputTokens,
		cachedTokens:  self.cachedTokens,
		writtenTokens: self.writtenTokens,
	}, true
}

func readNumber(fragment []byte, key []byte, current int64, hasCurrent bool) (int64, bool) {
	remaining := fragment
	for {
		index := bytes.Index(remaining, key)
		if index < 0 {
			return current, hasCurrent
		}
		start := index + len(key)
		for start < len(remaining) && (remaining[start] == ' ' || remaining[start] == '\t') {
			start++
		}
		if start < len(remaining) && remaining[start] >= '0' && remaining[start] <= '9' {
			var number int64
			for ; start < len(remaining) && remaining[start] >= '0' && remaining[start] <= '9'; start++ {
				number = number*10 + int64(remaining[start]-'0')
			}
			current = number
			hasCurrent = true
		}
		remaining = remaining[index+len(key):]
	}
}

func trailingBytes(destination []byte, fragment []byte) []byte {
	if len(fragment) >= fragmentOverlap {
		return append(destination[:0], fragment[len(fragment)-fragmentOverlap:]...)
	}
	if len(destination)+len(fragment) > fragmentOverlap {
		destination = destination[len(destination)+len(fragment)-fragmentOverlap:]
	}
	return append(destination, fragment...)
}

func readAnalysisCache(path string) analysisCache {
	freshCache := analysisCache{Format: analysisCacheFormat, Sessions: map[string]cachedSession{}}
	if path == "" {
		return freshCache
	}

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return freshCache
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		return freshCache
	}

	var readCache analysisCache
	if json.Unmarshal(data, &readCache) != nil || readCache.Format != analysisCacheFormat || readCache.Sessions == nil {
		return freshCache
	}
	return readCache
}

func writeAnalysisCache(path string, cache analysisCache) error {
	if path == "" {
		return nil
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	name := filepath.Base(path)
	temporaryName := fmt.Sprintf("%s.%d.tmp", name, os.Getpid())
	if err := root.WriteFile(temporaryName, data, 0o600); err != nil {
		return err
	}
	defer func() { _ = root.Remove(temporaryName) }()
	return root.Rename(temporaryName, name)
}

func writeJSON(analysis Analysis, writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "    ")
	return encoder.Encode(analysis)
}

type reportRow struct {
	cells      []string
	appearance style.Style
}

const reportColumnGap = 2

func writeText(analysis Analysis, writer io.Writer) error {
	restoreStyle := style.Init(writer)
	defer restoreStyle()

	if len(analysis.PromptCache.Providers) == 0 {
		_, err := fmt.Fprintln(writer, style.Subtle("No cache usage was recorded."))
		return err
	}

	cacheRows := make([]reportRow, 0, len(analysis.PromptCache.Providers)+1)
	for _, statistics := range analysis.PromptCache.Providers {
		cacheRows = append(cacheRows, cacheRow(model.ProviderName(statistics.Provider), statistics, style.Answer))
	}
	if len(analysis.PromptCache.Providers) > 1 {
		cacheRows = append(cacheRows, cacheRow("Total", analysis.PromptCache.Total, style.Information))
	}
	if err := writeReportTable(
		writer,
		[]string{"Provider", "Sessions", "Requests", "Hits", "Misses", "Hit Rate", "Input Cached", "Tokens Read"},
		cacheRows,
	); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(writer); err != nil {
		return err
	}
	contextRows := make([]reportRow, 0, len(analysis.PromptCache.Providers)+1)
	for _, statistics := range analysis.PromptCache.Providers {
		contextRows = append(contextRows, contextRow(model.ProviderName(statistics.Provider), statistics, style.Answer))
	}
	if len(analysis.PromptCache.Providers) > 1 {
		contextRows = append(contextRows, contextRow("Total", analysis.PromptCache.Total, style.Information))
	}
	return writeReportTable(
		writer,
		[]string{"Provider", "Average Input", "Peak Input", "Total Input", "Cache Reads", "Cache Writes", "Fresh Input"},
		contextRows,
	)
}

func cacheRow(name string, statistics CacheStatistics, appearance style.Style) reportRow {
	return reportRow{
		appearance: appearance,
		cells: []string{
			name,
			strconv.Itoa(statistics.Sessions),
			strconv.Itoa(statistics.Requests),
			strconv.Itoa(statistics.Hits),
			strconv.Itoa(statistics.Misses),
			percentage(statistics.Hits, statistics.Requests),
			percentage(statistics.CachedTokens, statistics.InputTokens),
			formatTokenCount(statistics.CachedTokens),
		},
	}
}

func contextRow(name string, statistics CacheStatistics, appearance style.Style) reportRow {
	averageInputTokens := int64(0)
	if statistics.Requests > 0 {
		averageInputTokens = statistics.InputTokens / int64(statistics.Requests)
	}
	freshInputTokens := max(
		statistics.InputTokens-statistics.CachedTokens-statistics.WrittenTokens,
		0,
	)
	return reportRow{
		appearance: appearance,
		cells: []string{
			name,
			formatTokenCount(averageInputTokens),
			formatTokenCount(statistics.PeakInputTokens),
			formatTokenCount(statistics.InputTokens),
			formatTokenCount(statistics.CachedTokens),
			formatTokenCount(statistics.WrittenTokens),
			formatTokenCount(freshInputTokens),
		},
	}
}

func writeReportTable(writer io.Writer, header []string, rows []reportRow) error {
	columnWidths := make([]int, len(header))
	for index, cell := range header {
		columnWidths[index] = style.Width(cell)
	}
	for _, row := range rows {
		for index, cell := range row.cells {
			columnWidths[index] = max(columnWidths[index], style.Width(cell))
		}
	}

	if err := writeReportRow(writer, reportRow{cells: header, appearance: style.Column}, columnWidths); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeReportRow(writer, row, columnWidths); err != nil {
			return err
		}
	}
	return nil
}

func writeReportRow(writer io.Writer, row reportRow, columnWidths []int) error {
	var line strings.Builder
	for index, cell := range row.cells {
		padding := columnWidths[index] - style.Width(cell)
		if index == 0 {
			line.WriteString(cell)
			line.WriteString(strings.Repeat(" ", padding))
		} else {
			line.WriteString(strings.Repeat(" ", padding))
			line.WriteString(cell)
		}
		if index < len(row.cells)-1 {
			line.WriteString(strings.Repeat(" ", reportColumnGap))
		}
	}
	_, err := fmt.Fprintln(writer, row.appearance(line.String()))
	return err
}

const billionTokens = 1_000_000_000

func formatTokenCount(tokens int64) string {
	if tokens < billionTokens {
		return util.FormatTokenCount(tokens)
	}
	count := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", float64(tokens)/billionTokens), "0"), ".")
	return count + "B"
}

func percentage[Count ~int | ~int64](part Count, whole Count) string {
	if whole <= 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(part)*100/float64(whole))
}
