package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	cfgpkg "clawgo/pkg/config"
)

func TestHandleWebUIConfigRequiresConfirmForProviderAPIBaseChange(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")

	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	cfg.Providers.Proxy.APIBase = "https://old.example/v1"
	cfg.Providers.Proxy.APIKey = "test-key"
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	bodyCfg := cfgpkg.DefaultConfig()
	bodyCfg.Logging.Enabled = false
	bodyCfg.Providers.Proxy.APIBase = "https://new.example/v1"
	bodyCfg.Providers.Proxy.APIKey = "test-key"
	body, err := json.Marshal(bodyCfg)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)

	req := httptest.NewRequest(http.MethodPost, "/webui/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"requires_confirm":true`) {
		t.Fatalf("expected requires_confirm response, got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `providers.proxy.api_base`) {
		t.Fatalf("expected providers.proxy.api_base in changed_fields, got: %s", rec.Body.String())
	}
}

func TestHandleWebUIConfigRequiresConfirmForCustomProviderSecretChange(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")

	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	cfg.Providers.Proxies["backup"] = cfgpkg.ProviderConfig{
		APIBase:    "https://backup.example/v1",
		APIKey:     "old-secret",
		Models:     []string{"backup-model"},
		Auth:       "bearer",
		TimeoutSec: 30,
	}
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	bodyCfg := cfgpkg.DefaultConfig()
	bodyCfg.Logging.Enabled = false
	bodyCfg.Providers.Proxies["backup"] = cfgpkg.ProviderConfig{
		APIBase:    "https://backup.example/v1",
		APIKey:     "new-secret",
		Models:     []string{"backup-model"},
		Auth:       "bearer",
		TimeoutSec: 30,
	}
	body, err := json.Marshal(bodyCfg)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)

	req := httptest.NewRequest(http.MethodPost, "/webui/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"requires_confirm":true`) {
		t.Fatalf("expected requires_confirm response, got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `providers.proxies.backup.api_key`) {
		t.Fatalf("expected providers.proxies.backup.api_key in changed_fields, got: %s", rec.Body.String())
	}
}
