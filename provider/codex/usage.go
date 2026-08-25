package codex

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"crdx.org/io/agent"
)

const (
	headerPrefix     = "X-Codex"
	usedSuffix       = "-Used-Percent"
	windowSuffix     = "-Window-Minutes"
	resetAtSuffix    = "-Reset-At"
	resetAfterSuffix = "-Reset-After-Seconds"
	limitNameSuffix  = "-Limit-Name"

	primaryPart   = "-Primary"
	secondaryPart = "-Secondary"
)

type limitBucket struct {
	prefix string
	scope  string
}

func (self *Client) IsAvailable() bool {
	self.usageMutex.Lock()
	defer self.usageMutex.Unlock()

	return self.usageWindows != nil
}

func (self *Client) UsageWindows(context.Context) ([]agent.UsageWindow, error) {
	self.usageMutex.Lock()
	defer self.usageMutex.Unlock()

	return slices.Clone(self.usageWindows), nil
}

func (self *Client) recordUsageWindows(header http.Header, now time.Time) {
	var windows []agent.UsageWindow

	for _, bucket := range limitBuckets(header) {
		for _, part := range []string{primaryPart, secondaryPart} {
			if window, ok := usageWindow(header, bucket, part, now); ok {
				windows = append(windows, window)
			}
		}
	}

	if windows == nil {
		return
	}

	self.usageMutex.Lock()
	defer self.usageMutex.Unlock()

	self.usageWindows = windows
}

func limitBuckets(header http.Header) []limitBucket {
	buckets := []limitBucket{{prefix: headerPrefix}}

	var named []string

	for name := range header {
		prefix, found := strings.CutSuffix(http.CanonicalHeaderKey(name), primaryPart+usedSuffix)
		if found && strings.HasPrefix(prefix, headerPrefix+"-") {
			named = append(named, prefix)
		}
	}

	slices.Sort(named)

	for _, prefix := range named {
		buckets = append(buckets, limitBucket{prefix: prefix, scope: scopeName(header, prefix)})
	}

	return buckets
}

func scopeName(header http.Header, prefix string) string {
	if name := strings.TrimSpace(header.Get(prefix + limitNameSuffix)); name != "" {
		return strings.ToLower(name)
	}

	return strings.ToLower(strings.TrimPrefix(prefix, headerPrefix+"-"))
}

func usageWindow(
	header http.Header, bucket limitBucket, part string, now time.Time,
) (agent.UsageWindow, bool) {
	used, err := strconv.ParseFloat(header.Get(bucket.prefix+part+usedSuffix), 64)
	if err != nil {
		return agent.UsageWindow{}, false
	}

	minutes, err := strconv.Atoi(header.Get(bucket.prefix + part + windowSuffix))
	if err != nil || minutes <= 0 {
		return agent.UsageWindow{}, false
	}

	return agent.UsageWindow{
		Duration: time.Duration(minutes) * time.Minute,
		Percent:  used,
		ResetsAt: resetTime(header, bucket.prefix+part, now),
		Scope:    bucket.scope,
	}, true
}

func resetTime(header http.Header, prefix string, now time.Time) time.Time {
	if at, err := strconv.ParseInt(header.Get(prefix+resetAtSuffix), 10, 64); err == nil && at > 0 {
		return time.Unix(at, 0).UTC()
	}

	seconds, err := strconv.Atoi(header.Get(prefix + resetAfterSuffix))
	if err != nil || seconds <= 0 {
		return time.Time{}
	}

	return now.Add(time.Duration(seconds) * time.Second)
}
