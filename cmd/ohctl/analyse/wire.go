package analyse

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
)

type wireLineKind int

const (
	unknownWireLine wireLineKind = iota
	responsesWireLine
	anthropicWireLine
	anthropicDeltaWireLine
	chatCompletionsWireLine
)

type wireLine struct {
	kind             wireLineKind
	inputTokens      int64
	cachedTokens     int64
	writtenTokens    int64
	outputTokens     int64
	hasInputTokens   bool
	hasCachedTokens  bool
	hasWrittenTokens bool
	hasOutputTokens  bool
}

var (
	responsesCompletedType = []byte(`"type":"response.completed"`)
	responsesDoneType      = []byte(`"type":"response.done"`)
	anthropicStartType     = []byte(`"type":"message_start"`)
	anthropicDeltaType     = []byte(`"type":"message_delta"`)
	inputTokensKey         = []byte(`"input_tokens":`)
	promptTokensKey        = []byte(`"prompt_tokens":`)
	cachedTokensKey        = []byte(`"cached_tokens":`)
	cacheReadTokensKey     = []byte(`"cache_read_input_tokens":`)
	cacheWriteTokensKey    = []byte(`"cache_write_tokens":`)
	cacheCreationTokensKey = []byte(`"cache_creation_input_tokens":`)
	outputTokensKey        = []byte(`"output_tokens":`)
	completionTokensKey    = []byte(`"completion_tokens":`)
)

const fragmentOverlap = 96

func readTranscript(reader io.Reader) (string, []usageReport, error) {
	bufferedReader := bufio.NewReader(reader)
	provider := ""
	var reports []usageReport
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
			if len(overlap) > 0 {
				boundary := append(slices.Clone(overlap), fragment[:min(len(fragment), fragmentOverlap)]...)
				line.inspect(boundary)
			}
			line.inspect(fragment)
			overlap = trailingBytes(overlap, fragment)
		}

		isLineEnd := !errors.Is(err, bufio.ErrBufferFull)
		if isLineEnd {
			reports = line.reported(reports)
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
	case bytes.Contains(fragment, anthropicDeltaType):
		self.kind = anthropicDeltaWireLine
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
		self.readOutputTokens(fragment, outputTokensKey)
	case anthropicWireLine:
		self.inputTokens, self.hasInputTokens = readNumber(fragment, inputTokensKey, self.inputTokens, self.hasInputTokens)
		self.cachedTokens, self.hasCachedTokens = readNumber(fragment, cacheReadTokensKey, self.cachedTokens, self.hasCachedTokens)
		self.writtenTokens, self.hasWrittenTokens = readNumber(
			fragment,
			cacheCreationTokensKey,
			self.writtenTokens,
			self.hasWrittenTokens,
		)
		self.readOutputTokens(fragment, outputTokensKey)
	case anthropicDeltaWireLine:
		self.readOutputTokens(fragment, outputTokensKey)
	case chatCompletionsWireLine:
		self.inputTokens, self.hasInputTokens = readNumber(fragment, promptTokensKey, self.inputTokens, self.hasInputTokens)
		self.cachedTokens, self.hasCachedTokens = readNumber(fragment, cachedTokensKey, self.cachedTokens, self.hasCachedTokens)
		self.writtenTokens, self.hasWrittenTokens = readNumber(
			fragment,
			cacheWriteTokensKey,
			self.writtenTokens,
			self.hasWrittenTokens,
		)
		self.readOutputTokens(fragment, completionTokensKey)
	case unknownWireLine:
	}
}

func (self *wireLine) readOutputTokens(fragment []byte, key []byte) {
	self.outputTokens, self.hasOutputTokens = readNumber(fragment, key, self.outputTokens, self.hasOutputTokens)
}

func (self *wireLine) reported(reports []usageReport) []usageReport {
	report, isReported := self.report()
	switch {
	case !isReported:
		return reports
	case self.kind != anthropicDeltaWireLine:
		return append(reports, report)
	case len(reports) > 0:
		reports[len(reports)-1].outputTokens = report.outputTokens
	}

	return reports
}

func (self *wireLine) report() (usageReport, bool) {
	if self.kind == anthropicDeltaWireLine {
		return usageReport{outputTokens: self.outputTokens}, self.hasOutputTokens
	}
	if !self.hasInputTokens {
		return usageReport{}, false
	}
	if self.kind == anthropicWireLine {
		self.inputTokens += self.cachedTokens + self.writtenTokens
	}
	return usageReport{
		inputTokens:   self.inputTokens,
		cachedTokens:  self.cachedTokens,
		writtenTokens: self.writtenTokens,
		outputTokens:  self.outputTokens,
	}, true
}

func readNumber(fragment []byte, key []byte, current int64, hasCurrent bool) (int64, bool) {
	remainingFragment := fragment
	for {
		index := bytes.Index(remainingFragment, key)
		if index < 0 {
			return current, hasCurrent
		}
		start := index + len(key)
		for start < len(remainingFragment) && (remainingFragment[start] == ' ' || remainingFragment[start] == '\t') {
			start++
		}
		if start < len(remainingFragment) && remainingFragment[start] >= '0' && remainingFragment[start] <= '9' {
			var number int64
			for ; start < len(remainingFragment) && remainingFragment[start] >= '0' && remainingFragment[start] <= '9'; start++ {
				number = number*10 + int64(remainingFragment[start]-'0')
			}
			current = number
			hasCurrent = true
		}
		remainingFragment = remainingFragment[index+len(key):]
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
