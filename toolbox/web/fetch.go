package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"crdx.org/io/internal/html2md"
	"crdx.org/io/tool"
)

const (
	fetchTimeout  = 30 * time.Second
	maxFetchBytes = 8 * 1024 * 1024
	userAgent     = "oh web_fetch"
)

type FetchArgs struct {
	URL  string `json:"url"`
	Type string `json:"type"`
}

func defaultFetchClient() *http.Client {
	return &http.Client{Timeout: fetchTimeout}
}

func newFetch(allowed func() bool, client *http.Client) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "web_fetch",
			Description: "fetch a web page as markdown, clean HTML, text, or raw HTML",
			Schema: tool.Schema{
				tool.String("url", "URL to fetch"),
				tool.String("type", "one of: markdown, clean_html, text, raw"),
			},
		},
		func(args FetchArgs) (string, string) { return args.URL, args.Type },
	).
		Validate(validateFetch).
		Focuses(func(call tool.ToolCall) string { return call.Subject() }).
		IsEmbarrassinglyParallel().
		ChangesNothing().
		Run(runAfterAccess(allowed, func(ctx context.Context, args FetchArgs) (tool.ToolCallResult, error) {
			output, err := fetchPage(ctx, client, args)
			if err != nil {
				return tool.ToolCallResult{}, err
			}

			return outputResult(output)
		}))
}

func validateFetch(args FetchArgs) error {
	address, err := url.Parse(args.URL)
	if err != nil || address.Host == "" || (address.Scheme != "http" && address.Scheme != "https") {
		return errors.New("url must be an absolute HTTP or HTTPS URL")
	}

	switch args.Type {
	case "markdown", "clean_html", "text", "raw":
		return nil
	default:
		return errors.New("type must be one of: markdown, clean_html, text, raw")
	}
}

func fetchPage(ctx context.Context, client *http.Client, args FetchArgs) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, args.URL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "text/html, application/xhtml+xml")

	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("web fetch failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	contents, err := io.ReadAll(io.LimitReader(response.Body, maxFetchBytes+1))
	if err != nil {
		return "", fmt.Errorf("could not read the web page: %w", err)
	}
	if len(contents) > maxFetchBytes {
		return "", fmt.Errorf("web page is larger than the %d-byte limit", maxFetchBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("web fetch failed with status %d: %s", response.StatusCode, strings.TrimSpace(string(contents)))
	}
	if args.Type == "raw" {
		return string(contents), nil
	}

	document, err := html.Parse(bytes.NewReader(contents))
	if err != nil {
		return "", fmt.Errorf("could not parse the web page: %w", err)
	}
	removeUnwantedNodes(document)

	root := findElement(document, "body")
	if root == nil {
		root = document
	}

	switch args.Type {
	case "clean_html":
		return renderChildren(root)
	case "text":
		return renderText(root), nil
	case "markdown":
		return html2md.Convert(root), nil
	default:
		panic("validated fetch type became unknown")
	}
}
