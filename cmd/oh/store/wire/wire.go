// Package wire records censored logical HTTP exchanges.
package wire

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"crdx.org/io/internal/req"
)

const redacted = "[REDACTED]"

var bearerPattern = regexp.MustCompile(`(?i)bearer[ \t]+[^\s"']+`)

// Meta identifies the session whose exchanges are recorded.
type Meta struct {
	ID, Model, Effort, Provider, Workspace string
	Started                                time.Time
}

// Recorder appends censored logical HTTP exchanges.
type Recorder struct {
	mutex     sync.Mutex
	file      *os.File
	hasFailed bool
	next      int
	report    func(error)
}

// Open opens or creates an HTTP transcript.
func Open(path string, meta Meta, report func(error)) (*Recorder, error) {
	recorder := &Recorder{report: report, next: 1}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o600) //nolint:gosec // the parent store supplies the fixed bundle path
	if err != nil {
		return nil, err
	}
	recorder.file = file

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.Size() == 0 {
		if _, err := fmt.Fprintf(file, "# HTTP transcript\n# id: %s\n# started: %s\n# model: %s\n# effort: %s\n# provider: %s\n# workspace: %s\n\n", meta.ID, meta.Started.UTC().Format(time.RFC3339Nano), meta.Model, meta.Effort, meta.Provider, meta.Workspace); err != nil {
			_ = file.Close()
			return nil, err
		}
	} else {
		if _, err := file.Seek(0, 0); err != nil {
			_ = file.Close()
			return nil, err
		}
		reader := bufio.NewReader(file)
		for {
			line, readError := reader.ReadString('\n')
			var sequence int
			if _, err := fmt.Sscanf(line, "# exchange %d start", &sequence); err == nil && sequence >= recorder.next {
				recorder.next = sequence + 1
			}
			if errors.Is(readError, io.EOF) {
				break
			}
			if readError != nil {
				_ = file.Close()
				return nil, readError
			}
		}
		if _, err := file.Seek(0, 2); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	return recorder, nil
}

// Start records a request and returns its exchange observer.
func (self *Recorder) Start(request req.Request) req.ExchangeObserver {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	sequence := self.next
	self.next++
	exchange := &exchange{recorder: self, sequence: sequence, startedAt: request.Started}
	self.write(fmt.Sprintf("# exchange %d start %s\n> %s %s %s\n", sequence, request.Started.UTC().Format(time.RFC3339Nano), request.Method, request.URL, request.Protocol))
	self.writeHeaders(">", request.Header)
	self.write("\n")
	self.write(string(censorBody(request.Body, request.Header.Get("Content-Type"))))
	self.write("\n")
	return exchange
}

// Close closes the HTTP transcript.
func (self *Recorder) Close() error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.file == nil {
		return nil
	}
	err := self.file.Close()
	self.file = nil
	if err != nil {
		self.fail(err)
	}
	return err
}

func (self *Recorder) writeHeaders(prefix string, headers http.Header) {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		for _, value := range headers.Values(name) {
			if isSensitiveName(name) {
				value = redacted
			} else {
				value = censorBearer(value)
			}
			self.write(fmt.Sprintf("%s %s: %s\n", prefix, name, value))
		}
	}
}

func (self *Recorder) write(value string) {
	if self.hasFailed || self.file == nil {
		return
	}
	if _, err := self.file.WriteString(value); err != nil {
		self.fail(err)
	}
}

func (self *Recorder) fail(err error) {
	if self.hasFailed {
		return
	}
	self.hasFailed = true
	if self.file != nil {
		_ = self.file.Close()
		self.file = nil
	}
	if self.report != nil {
		self.report(fmt.Errorf("wire.http recording disabled: %w", err))
	}
}

type exchange struct {
	recorder     *Recorder
	sequence     int
	startedAt    time.Time
	contentType  string
	body         bytes.Buffer
	hasResponded bool
	isStreaming  bool
}

func (self *exchange) Response(response req.Response) {
	self.recorder.mutex.Lock()
	defer self.recorder.mutex.Unlock()
	self.hasResponded = true
	self.contentType = response.Header.Get("Content-Type")
	self.isStreaming = strings.Contains(strings.ToLower(self.contentType), "event-stream")
	self.recorder.write(fmt.Sprintf("\n< %s %s\n", response.Protocol, response.Status))
	self.recorder.writeHeaders("<", response.Header)
	self.recorder.write("\n")
}

func (self *exchange) Body(body []byte) {
	if !self.isStreaming {
		_, _ = self.body.Write(body)
		return
	}

	self.recorder.mutex.Lock()
	defer self.recorder.mutex.Unlock()
	_, _ = self.body.Write(body)
	for {
		buffered := self.body.Bytes()
		lineEnd := bytes.IndexByte(buffered, '\n')
		if lineEnd < 0 {
			return
		}
		line := bytes.Clone(buffered[:lineEnd+1])
		self.body.Next(lineEnd + 1)
		self.recorder.write(string(censorBody(line, self.contentType)))
	}
}

func (self *exchange) Finish(finished time.Time, err error, incomplete bool) {
	self.recorder.mutex.Lock()
	defer self.recorder.mutex.Unlock()
	self.recorder.write(string(censorBody(self.body.Bytes(), self.contentType)))
	self.recorder.write("\n")
	state := "completed"
	switch {
	case incomplete:
		state = "incomplete close"
	case errors.Is(err, context.Canceled):
		state = "cancelled"
	case err != nil && !self.hasResponded:
		state = "transport error: " + censorBearer(err.Error())
	case err != nil:
		state = "read error: " + censorBearer(err.Error())
	}
	self.recorder.write(fmt.Sprintf("# exchange %d end %s elapsed=%s %s\n\n", self.sequence, finished.UTC().Format(time.RFC3339Nano), finished.Sub(self.startedAt), state))
}

func censorBody(body []byte, contentType string) []byte {
	if len(body) == 0 {
		return nil
	}
	lowerType := strings.ToLower(contentType)
	if strings.Contains(lowerType, "json") {
		return censorJSON(body)
	}
	if strings.Contains(lowerType, "x-www-form-urlencoded") {
		values, err := url.ParseQuery(string(body))
		if err == nil {
			for key := range values {
				if isSensitiveName(key) {
					values[key] = []string{redacted}
				}
			}
			return []byte(values.Encode())
		}
	}
	if strings.Contains(lowerType, "event-stream") {
		lines := strings.Split(string(body), "\n")
		for i, line := range lines {
			if data, found := strings.CutPrefix(line, "data:"); found {
				space := ""
				if remaining, found := strings.CutPrefix(data, " "); found {
					space, data = " ", remaining
				}
				lines[i] = "data:" + space + string(censorJSON([]byte(data)))
			}
		}
		return []byte(censorBearer(strings.Join(lines, "\n")))
	}
	return []byte(censorBearer(string(body)))
}

func censorJSON(body []byte) []byte {
	var value any
	if json.Unmarshal(body, &value) != nil {
		return []byte(censorBearer(string(body)))
	}
	if !containsSensitiveValue(value) {
		return []byte(censorBearer(string(body)))
	}
	censorValue(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte(censorBearer(string(body)))
	}
	return []byte(censorBearer(string(encoded)))
}

func containsSensitiveValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if isSensitiveName(key) || containsSensitiveValue(item) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(typed, containsSensitiveValue)
	}
	return false
}

func censorValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if isSensitiveName(key) {
				typed[key] = redacted
			} else {
				censorValue(item)
			}
		}
	case []any:
		for _, item := range typed {
			censorValue(item)
		}
	}
}

func isSensitiveName(name string) bool {
	normalised := strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(name))
	switch normalised {
	case
		"authorization",
		"proxyauthorization",
		"cookie",
		"setcookie",
		"apikey",
		"xapikey",
		"openaiaccountid",
		"xopenaiaccountid",
		"chatgptaccountid",
		"accesstoken",
		"refreshtoken",
		"idtoken",
		"clientsecret",
		"token",
		"password":
		return true
	}
	return strings.Contains(normalised, "credential")
}

func censorBearer(value string) string {
	return bearerPattern.ReplaceAllString(value, "Bearer "+redacted)
}
