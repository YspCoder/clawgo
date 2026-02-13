package browser

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultTimeout   = 30 * time.Second
	defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) clawgo/1.0 Safari/537.36"
)

// Browser provides a small, dependency-free wrapper around a Chromium-compatible
// binary. It is intentionally simple to keep clawgo deployable as a single Go
// binary without extra Go browser dependencies.
type Browser struct {
	binPath   string
	timeout   time.Duration
	userAgent string
}

func New() *Browser {
	return &Browser{
		binPath:   detectChromeBinary(),
		timeout:   defaultTimeout,
		userAgent: defaultUserAgent,
	}
}

func (b *Browser) SetTimeout(timeout time.Duration) {
	if timeout > 0 {
		b.timeout = timeout
	}
}

func (b *Browser) Available() bool {
	return b.binPath != ""
}

func (b *Browser) Screenshot(ctx context.Context, url, outputPath string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("url is required")
	}
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("output path is required")
	}
	if b.binPath == "" {
		return fmt.Errorf("no chromium-compatible browser found (tried chromium-browser/chromium/google-chrome/chrome)")
	}

	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, b.binPath,
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--hide-scrollbars",
		"--window-size=1280,720",
		"--user-agent="+b.userAgent,
		"--screenshot="+outputPath,
		url,
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("screenshot failed: %w, output=%s", err, truncate(string(out), 512))
	}
	return nil
}

func (b *Browser) Content(ctx context.Context, url string) (string, error) {
	if strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("url is required")
	}
	if b.binPath == "" {
		return "", fmt.Errorf("no chromium-compatible browser found (tried chromium-browser/chromium/google-chrome/chrome)")
	}

	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, b.binPath,
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--hide-scrollbars",
		"--user-agent="+b.userAgent,
		"--dump-dom",
		url,
	)

	output, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("content fetch failed: %w, stderr=%s", err, truncate(string(ee.Stderr), 512))
		}
		return "", fmt.Errorf("content fetch failed: %w", err)
	}

	return string(output), nil
}

func detectChromeBinary() string {
	candidates := []string{
		"chromium-browser",
		"chromium",
		"google-chrome",
		"chrome",
	}
	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
