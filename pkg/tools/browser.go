package tools

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

type BrowserTool struct {
	chromePath string
	timeout    time.Duration
}

func NewBrowserTool() *BrowserTool {
	return &BrowserTool{
		timeout: 30 * time.Second,
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
	// 基于 CLI 的简单实现：使用 chromium-browser --headless
	outputPath := fmt.Sprintf("/tmp/screenshot_%d.png", time.Now().UnixNano())
	cmd := exec.CommandContext(ctx, "chromium-browser",
		"--headless",
		"--disable-gpu",
		"--no-sandbox",
		"--screenshot="+outputPath,
		url)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to take screenshot: %w (ensure chromium-browser is installed)", err)
	}

	return fmt.Sprintf("Screenshot saved to: %s", outputPath), nil
}

func (t *BrowserTool) fetchDynamicContent(ctx context.Context, url string) (string, error) {
	// 简单实现：dump-dom
	cmd := exec.CommandContext(ctx, "chromium-browser",
		"--headless",
		"--disable-gpu",
		"--no-sandbox",
		"--dump-dom",
		url)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch content: %w", err)
	}

	return string(output), nil
}
