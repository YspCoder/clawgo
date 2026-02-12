package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	userAgent = "Mozilla/5.0 (compatible; clawgo/1.0)"
)

type WebSearchTool struct {
	apiKey     string
	maxResults int
}

func NewWebSearchTool(apiKey string, maxResults int) *WebSearchTool {
	if maxResults <= 0 || maxResults > 10 {
		maxResults = 5
	}
	return &WebSearchTool{
		apiKey:     apiKey,
		maxResults: maxResults,
	}
}

func (t *WebSearchTool) Name() string {
	return "web_search"
}

func (t *WebSearchTool) Description() string {
	return "Search the web for current information. Returns titles, URLs, and snippets from search results."
}

func (t *WebSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query",
			},
			"count": map[string]interface{}{
				"type":        "integer",
				"description": "Number of results (1-10)",
				"minimum":     1.0,
				"maximum":     10.0,
			},
		},
		"required": []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t.apiKey == "" {
		return "Error: BRAVE_API_KEY not configured", nil
	}

	query, ok := args["query"].(string)
	if !ok {
		return "", fmt.Errorf("query is required")
	}

	count := t.maxResults
	if c, ok := args["count"].(float64); ok {
		if int(c) > 0 && int(c) <= 10 {
			count = int(c)
		}
	}

	searchURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(query), count)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", t.apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var searchResp struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}

	if err := json.Unmarshal(body, &searchResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	results := searchResp.Web.Results
	if len(results) == 0 {
		return fmt.Sprintf("No results for: %s", query), nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Results for: %s", query))
	for i, item := range results {
		if i >= count {
			break
		}
		lines = append(lines, fmt.Sprintf("%d. %s\n   %s", i+1, item.Title, item.URL))
		if item.Description != "" {
			lines = append(lines, fmt.Sprintf("   %s", item.Description))
		}
	}

	return strings.Join(lines, "\n"), nil
}

type WebFetchTool struct {
	maxChars int
}

func NewWebFetchTool(maxChars int) *WebFetchTool {
	if maxChars <= 0 {
		maxChars = 50000
	}
	return &WebFetchTool{
		maxChars: maxChars,
	}
}

func (t *WebFetchTool) Name() string {
	return "web_fetch"
}

func (t *WebFetchTool) Description() string {
	return "Fetch a URL and extract readable content (HTML to Markdown). Preserves structure (headers, links, code blocks) for better reading."
}

func (t *WebFetchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "URL to fetch",
			},
			"maxChars": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum characters to extract",
				"minimum":     100.0,
			},
		},
		"required": []string{"url"},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	urlStr, ok := args["url"].(string)
	if !ok {
		return "", fmt.Errorf("url is required")
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("only http/https URLs are allowed")
	}

	if parsedURL.Host == "" {
		return "", fmt.Errorf("missing domain in URL")
	}

	maxChars := t.maxChars
	if mc, ok := args["maxChars"].(float64); ok {
		if int(mc) > 100 {
			maxChars = int(mc)
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  false,
			TLSHandshakeTimeout: 15 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")

	var text, extractor string

	if strings.Contains(contentType, "application/json") {
		var jsonData interface{}
		if err := json.Unmarshal(body, &jsonData); err == nil {
			formatted, _ := json.MarshalIndent(jsonData, "", "  ")
			text = string(formatted)
			extractor = "json"
		} else {
			text = string(body)
			extractor = "raw"
		}
	} else if strings.Contains(contentType, "text/html") || len(body) > 0 &&
		(strings.HasPrefix(string(body), "<!DOCTYPE") || strings.HasPrefix(strings.ToLower(string(body)), "<html")) {
		text = t.extractMarkdown(string(body))
		extractor = "html-to-markdown"
	} else {
		text = string(body)
		extractor = "raw"
	}

	truncated := len(text) > maxChars
	if truncated {
		text = text[:maxChars]
	}

	result := map[string]interface{}{
		"url":       urlStr,
		"status":    resp.StatusCode,
		"extractor": extractor,
		"truncated": truncated,
		"length":    len(text),
		"text":      text,
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// extractMarkdown converts HTML to simplified Markdown using Regex.
// It's not perfect but much better than stripping everything.
func (t *WebFetchTool) extractMarkdown(html string) string {
	// 1. Remove Scripts and Styles
	re := regexp.MustCompile(`(?i)<script[\s\S]*?</script>`)
	html = re.ReplaceAllLiteralString(html, "")
	re = regexp.MustCompile(`(?i)<style[\s\S]*?</style>`)
	html = re.ReplaceAllLiteralString(html, "")
	re = regexp.MustCompile(`(?i)<!--[\s\S]*?-->`)
	html = re.ReplaceAllLiteralString(html, "")

	// 2. Pre-process block elements to ensure newlines
	// Replace </div>, </p>, </h1> etc. with newlines
	re = regexp.MustCompile(`(?i)</(div|p|h[1-6]|li|ul|ol|table|tr)>`)
	html = re.ReplaceAllString(html, "\n$0")

	// 3. Convert Headers
	re = regexp.MustCompile(`(?i)<h1[^>]*>(.*?)</h1>`)
	html = re.ReplaceAllString(html, "\n# $1\n")
	re = regexp.MustCompile(`(?i)<h2[^>]*>(.*?)</h2>`)
	html = re.ReplaceAllString(html, "\n## $1\n")
	re = regexp.MustCompile(`(?i)<h3[^>]*>(.*?)</h3>`)
	html = re.ReplaceAllString(html, "\n### $1\n")
	re = regexp.MustCompile(`(?i)<h[4-6][^>]*>(.*?)<.*?>`)
	html = re.ReplaceAllString(html, "\n#### $1\n")

	// 4. Convert Links: <a href="url">text</a> -> [text](url)
	re = regexp.MustCompile(`(?i)<a[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	html = re.ReplaceAllString(html, "[$2]($1)")

	// 5. Convert Images: <img src="url" alt="text"> -> ![text](url)
	re = regexp.MustCompile(`(?i)<img[^>]+src="([^"]+)"[^>]*alt="([^"]*)"[^>]*>`)
	html = re.ReplaceAllString(html, "![$2]($1)")

	// 6. Convert Bold/Italic
	re = regexp.MustCompile(`(?i)<(b|strong)>(.*?)</\1>`)
	html = re.ReplaceAllString(html, "**$2**")
	re = regexp.MustCompile(`(?i)<(i|em)>(.*?)</\1>`)
	html = re.ReplaceAllString(html, "*$2*")

	// 7. Convert Code Blocks
	re = regexp.MustCompile(`(?i)<pre[^>]*><code[^>]*>([\s\S]*?)</code></pre>`)
	html = re.ReplaceAllString(html, "\n```\n$1\n```\n")
	re = regexp.MustCompile(`(?i)<code[^>]*>(.*?)</code>`)
	html = re.ReplaceAllString(html, "`$1`")

	// 8. Convert Lists
	re = regexp.MustCompile(`(?i)<li[^>]*>(.*?)</li>`)
	html = re.ReplaceAllString(html, "- $1\n")

	// 9. Strip remaining tags
	re = regexp.MustCompile(`<[^>]+>`)
	html = re.ReplaceAllLiteralString(html, "")

	// 10. Decode HTML Entities (Basic ones)
	html = strings.ReplaceAll(html, "&nbsp;", " ")
	html = strings.ReplaceAll(html, "&amp;", "&")
	html = strings.ReplaceAll(html, "&lt;", "<")
	html = strings.ReplaceAll(html, "&gt;", ">")
	html = strings.ReplaceAll(html, "&quot;", "\"")
	html = strings.ReplaceAll(html, "&#39;", "'")

	// 11. Cleanup Whitespace
	// Collapse multiple spaces
	re = regexp.MustCompile(`[ \t]+`)
	html = re.ReplaceAllLiteralString(html, " ")
	// Collapse multiple newlines (max 2)
	re = regexp.MustCompile(`\n{3,}`)
	html = re.ReplaceAllLiteralString(html, "\n\n")

	return strings.TrimSpace(html)
}
