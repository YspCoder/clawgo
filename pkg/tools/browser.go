package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"clawgo/pkg/browser"
)

type BrowserTool struct {
	client *browser.Browser
}

func NewBrowserTool() *BrowserTool {
	client := browser.New()
	timeout := 30 * time.Second
	client.SetTimeout(timeout)

	return &BrowserTool{
		client: client,
	}
}

func (t *BrowserTool) Name() string {
	return "browser"
}

func (t *BrowserTool) Description() string {
	return "Control a headless browser to capture screenshots or fetch dynamic content using Chromium."
}

func (t *BrowserTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type": "string",
				"enum": []string{"screenshot", "content"},
			},
			"url": map[string]interface{}{
				"type": "string",
			},
		},
		"required": []string{"action", "url"},
	}
}

func (t *BrowserTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	action, _ := args["action"].(string)
	url, _ := args["url"].(string)

	switch action {
	case "screenshot":
		return t.takeScreenshot(ctx, url)
	case "content":
		return t.fetchDynamicContent(ctx, url)
	default:
		return "", fmt.Errorf("unknown browser action: %s", action)
	}
}

func (t *BrowserTool) takeScreenshot(ctx context.Context, url string) (string, error) {
	outputPath := fmt.Sprintf("/tmp/screenshot_%d.png", time.Now().UnixNano())
	if !t.client.Available() {
		return "", fmt.Errorf("failed to take screenshot: no chromium-compatible browser available")
	}

	if err := t.client.Screenshot(ctx, url, outputPath); err != nil {
		return "", err
	}

	return fmt.Sprintf("Screenshot saved to: %s", filepath.Clean(outputPath)), nil
}

func (t *BrowserTool) fetchDynamicContent(ctx context.Context, url string) (string, error) {
	if !t.client.Available() {
		return "", fmt.Errorf("failed to fetch content: no chromium-compatible browser available")
	}

	return t.client.Content(ctx, url)
}
