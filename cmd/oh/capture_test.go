package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	captureSecretPattern = regexp.MustCompile(`(?i)(authorization|bearer[[:space:]]|access_token|refresh_token|api[_-]?key|sk-[a-z0-9]{16,}|eyJ[a-z0-9_-]+\.)`)
	capturePIIPattern    = regexp.MustCompile(`(?i)(?:\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b|(?:^|[^0-9.])(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?)`)
)

func TestSanitisedWireCapturesContainNoCredentialsOrHostPaths(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "captures", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no sanitised wire captures")
	}

	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), func(t *testing.T) {
			capture, err := os.Open(path) //nolint:gosec // fixed testdata path
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = capture.Close() }()

			lines := bufio.NewScanner(capture)
			lines.Buffer(nil, 16*1024*1024)
			lineNumber := 0
			for lines.Scan() {
				lineNumber++
				line := lines.Text()
				if captureSecretPattern.MatchString(line) {
					t.Errorf("line %d contains a credential-shaped value", lineNumber)
				}
				if capturePIIPattern.MatchString(line) {
					t.Errorf("line %d contains an email or network address", lineNumber)
				}
				if strings.Contains(line, "/home/") || strings.Contains(line, "Dropbox/proj") {
					t.Errorf("line %d contains a host path", lineNumber)
				}

				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Errorf("line %d is not JSON: %v", lineNumber, err)
				} else {
					validateCapturePrivacy(t, record, "record")
				}
			}
			if err := lines.Err(); err != nil {
				t.Fatal(err)
			}
			if lineNumber < 2 {
				t.Errorf("capture has %d records, want a head and at least one exchange", lineNumber)
			}
		})
	}
}

func validateCapturePrivacy(t *testing.T, value any, location string) {
	t.Helper()

	switch typed := value.(type) {
	case map[string]any:
		if instructions, exists := typed["instructions"]; exists && instructions != "<system>" {
			t.Errorf("%s contains an unredacted system prompt", location)
		}
		role, _ := typed["role"].(string)
		if role == "system" && typed["content"] != "<system>" {
			t.Errorf("%s contains an unredacted system message", location)
		}
		if role == "assistant" {
			requireCapturePlaceholder(t, typed, "content", "<answer>", location)
			requireCapturePlaceholder(t, typed, "reasoning", "<reasoning>", location)
			requireCapturePlaceholder(t, typed, "reasoning_content", "<reasoning>", location)
		}
		if role == "tool" {
			requireCapturePlaceholder(t, typed, "content", "<tool-output>", location)
		}
		if workspace, exists := typed["workspace"]; exists && workspace != "<workspace>" {
			t.Errorf("%s contains an unredacted workspace", location)
		}
		if identifier, exists := typed["safety_identifier"]; exists && identifier != "<safety-identifier>" {
			t.Errorf("%s contains an unredacted safety identifier", location)
		}
		typeName, _ := typed["type"].(string)
		switch typeName {
		case "thinking", "thinking_delta":
			requireCapturePlaceholder(t, typed, "thinking", "<reasoning>", location)
		case "summary_text":
			requireCapturePlaceholder(t, typed, "text", "<reasoning>", location)
		case "output_text", "text_delta":
			requireCapturePlaceholder(t, typed, "text", "<answer>", location)
		case "input_json_delta":
			requireCapturePlaceholder(t, typed, "partial_json", "<arguments>", location)
		case "response.output_text.done":
			requireCapturePlaceholder(t, typed, "text", "<answer>", location)
		case "response.reasoning_summary_text.done":
			requireCapturePlaceholder(t, typed, "text", "<reasoning>", location)
		case "response.function_call_arguments.done":
			requireCapturePlaceholder(t, typed, "arguments", "<arguments>", location)
		}
		if role == "" && typeName == "" {
			requireCapturePlaceholder(t, typed, "content", "<answer>", location)
			requireCapturePlaceholder(t, typed, "reasoning", "<reasoning>", location)
			requireCapturePlaceholder(t, typed, "reasoning_content", "<reasoning>", location)
		}
		requireCapturePlaceholder(t, typed, "arguments", "<arguments>", location)
		requireCapturePlaceholder(t, typed, "partial_json", "<arguments>", location)
		requireCapturePlaceholder(t, typed, "output", "<tool-output>", location)
		for key, child := range typed {
			validateCapturePrivacy(t, child, location+"."+key)
		}
	case []any:
		for i, child := range typed {
			validateCapturePrivacy(t, child, location+"["+strconv.Itoa(i)+"]")
		}
	}
}

func requireCapturePlaceholder(
	t *testing.T,
	value map[string]any,
	field string,
	placeholder string,
	location string,
) {
	t.Helper()

	text, exists := value[field].(string)
	if exists && text != "" && text != placeholder {
		t.Errorf("%s.%s contains unredacted content", location, field)
	}
}

type wireCaptureRecord struct {
	Kind     string            `json:"kind"`
	Provider string            `json:"provider"`
	Response []json.RawMessage `json:"response"`
}

func TestSanitisedWireCaptureLifecyclesAreCoveredByScenarios(t *testing.T) {
	capturedFeatures := map[string]struct{}{}
	capturePaths, err := filepath.Glob(filepath.Join("testdata", "captures", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range capturePaths {
		capture, err := os.Open(path) //nolint:gosec // fixed testdata path
		if err != nil {
			t.Fatal(err)
		}

		provider := ""
		lines := bufio.NewScanner(capture)
		lines.Buffer(nil, 16*1024*1024)
		for lines.Scan() {
			var record wireCaptureRecord
			if err := json.Unmarshal(lines.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record.Kind == "capture" {
				provider = record.Provider
			}
			for _, payload := range record.Response {
				addWireLifecycleFeatures(capturedFeatures, provider, payload)
			}
		}
		if err := lines.Err(); err != nil {
			t.Fatal(err)
		}
		_ = capture.Close()
	}

	scenarioFeatures := map[string]struct{}{}
	scenarioPaths, err := filepath.Glob(filepath.Join("testdata", "scenarios", "*.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range scenarioPaths {
		scenario := readSessionGoldenScenario(t, path)
		provider := scenario.Provider
		if provider == "chat" {
			provider = "opencode-go"
		}
		for _, turn := range []sessionGoldenTurn{scenario.FirstTurn, scenario.ResumeTurn} {
			for _, response := range turn.Responses {
				for _, payload := range response.Events {
					addWireLifecycleFeatures(scenarioFeatures, provider, json.RawMessage(payload))
				}
			}
		}
	}

	for feature := range capturedFeatures {
		if _, covered := scenarioFeatures[feature]; !covered {
			t.Errorf("captured lifecycle feature %q has no generated scenario", feature)
		}
	}
}

func addWireLifecycleFeatures(features map[string]struct{}, provider string, payload json.RawMessage) {
	var done string
	if json.Unmarshal(payload, &done) == nil && done == "[DONE]" || string(payload) == "[DONE]" {
		features[provider+"/done"] = struct{}{}
		return
	}

	var event map[string]any
	if json.Unmarshal(payload, &event) != nil {
		return
	}

	switch provider {
	case "opencode-go":
		choices, _ := event["choices"].([]any)
		for _, rawChoice := range choices {
			choice, _ := rawChoice.(map[string]any)
			delta, _ := choice["delta"].(map[string]any)
			for _, field := range []string{"content", "reasoning_content", "reasoning", "refusal", "tool_calls"} {
				if value := delta[field]; value != nil && value != "" {
					features["chat/"+field] = struct{}{}
				}
			}
			if reason, _ := choice["finish_reason"].(string); reason != "" {
				features["chat/finish/"+reason] = struct{}{}
			}
		}

	case "anthropic":
		eventType, _ := event["type"].(string)
		switch eventType {
		case "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop", "error":
			features["anthropic/"+eventType] = struct{}{}
		}
		for _, field := range []string{"content_block", "delta"} {
			value, _ := event[field].(map[string]any)
			if valueType, _ := value["type"].(string); valueType != "" {
				features["anthropic/"+field+"/"+valueType] = struct{}{}
			}
		}

	case "codex":
		eventType, _ := event["type"].(string)
		switch eventType {
		case "response.output_text.delta", "response.refusal.delta", "response.output_text.done", "response.reasoning_summary_text.delta", "response.reasoning_summary_part.done", "response.reasoning_text.delta", "response.reasoning_text.done", "response.output_item.done", "response.completed", "response.done", "response.incomplete", "response.failed", "error":
			features["codex/"+eventType] = struct{}{}
		}
		if eventType == "response.output_item.done" {
			item, _ := event["item"].(map[string]any)
			if itemType, _ := item["type"].(string); itemType != "" {
				features["codex/item/"+itemType] = struct{}{}
			}
		}
		if eventType == "response.reasoning_summary_part.done" {
			part, _ := event["part"].(map[string]any)
			if partType, _ := part["type"].(string); partType != "" {
				features["codex/part/"+partType] = struct{}{}
			}
		}
	}
}
