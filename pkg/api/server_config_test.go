package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	cfgpkg "github.com/YspCoder/clawgo/pkg/config"
)

func TestHandleWebUIConfigRequiresConfirmForProviderAPIBaseChange(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")

	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	pc := cfg.Models.Providers["openai"]
	pc.APIBase = "https://old.example/v1"
	pc.APIKey = "test-key"
	cfg.Models.Providers["openai"] = pc
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	bodyCfg := cfgpkg.DefaultConfig()
	bodyCfg.Logging.Enabled = false
	bodyPC := bodyCfg.Models.Providers["openai"]
	bodyPC.APIBase = "https://new.example/v1"
	bodyPC.APIKey = "test-key"
	bodyCfg.Models.Providers["openai"] = bodyPC
	body, err := json.Marshal(bodyCfg)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)
	srv.SetConfigAfterHook(func() error { return nil })

	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"requires_confirm":true`) {
		t.Fatalf("expected requires_confirm response, got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `models.providers.openai.api_base`) {
		t.Fatalf("expected models.providers.openai.api_base in changed_fields, got: %s", rec.Body.String())
	}
}

func TestHandleWebUIConfigAcceptsStringConfirmRisky(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")

	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	pc := cfg.Models.Providers["openai"]
	pc.APIBase = "https://old.example/v1"
	pc.APIKey = "test-key"
	cfg.Models.Providers["openai"] = pc
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	bodyCfg := cfgpkg.DefaultConfig()
	bodyCfg.Logging.Enabled = false
	bodyPC := bodyCfg.Models.Providers["openai"]
	bodyPC.APIBase = "https://new.example/v1"
	bodyPC.APIKey = "test-key"
	bodyCfg.Models.Providers["openai"] = bodyPC
	bodyMap := map[string]interface{}{}
	raw, err := json.Marshal(bodyCfg)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	if err := json.Unmarshal(raw, &bodyMap); err != nil {
		t.Fatalf("unmarshal body map: %v", err)
	}
	bodyMap["confirm_risky"] = "true"
	body, err := json.Marshal(bodyMap)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)
	srv.SetConfigAfterHook(func() error { return nil })

	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestNormalizeCronJobParsesStringScheduleValues(t *testing.T) {
	t.Parallel()

	job := normalizeCronJob(map[string]interface{}{
		"schedule": map[string]interface{}{
			"kind":    "every",
			"everyMs": "60000",
		},
		"payload": map[string]interface{}{
			"message": "hello",
		},
	})
	if got, _ := job["expr"].(string); got == "" || !strings.Contains(got, "@every") {
		t.Fatalf("expected normalized @every expr, got %#v", job["expr"])
	}
}

func TestHandleWebUIConfigRequiresConfirmForCustomProviderSecretChange(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")

	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	cfg.Models.Providers["backup"] = cfgpkg.ProviderConfig{
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
	bodyCfg.Models.Providers["backup"] = cfgpkg.ProviderConfig{
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

	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"requires_confirm":true`) {
		t.Fatalf("expected requires_confirm response, got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `models.providers.backup.api_key`) {
		t.Fatalf("expected models.providers.backup.api_key in changed_fields, got: %s", rec.Body.String())
	}
}

func TestHandleWebUIConfigRunsReloadHookSynchronously(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)
	called := false
	srv.SetConfigAfterHook(func() error {
		called = true
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatalf("expected reload hook to run")
	}
}

func TestHandleWebUIConfigReturnsReloadHookError(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)
	srv.SetConfigAfterHook(func() error {
		return fmt.Errorf("reload boom")
	})

	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "reload failed") {
		t.Fatalf("expected reload failure in body, got: %s", rec.Body.String())
	}
}

func TestHandleWebUIConfigNormalizedGet(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	cfg.Agents.Agents["coder"] = cfgpkg.AgentConfig{
		Enabled:    true,
		Role:       "coding",
		PromptFile: "agents/coder/AGENT.md",
	}
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)
	req := httptest.NewRequest(http.MethodGet, "/api/config?mode=normalized", nil)
	rec := httptest.NewRecorder()

	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok=true, got %#v", payload)
	}
	configMap, _ := payload["config"].(map[string]interface{})
	coreMap, _ := configMap["core"].(map[string]interface{})
	if strings.TrimSpace(fmt.Sprintf("%v", coreMap["main_agent_id"])) != "main" {
		t.Fatalf("unexpected normalized config: %#v", payload)
	}
}

func TestHandleWebUIConfigNormalizedPost(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	body := map[string]interface{}{
		"confirm_risky": true,
		"core": map[string]interface{}{
			"default_provider": "openai",
			"default_model":    "gpt-5.4",
			"main_agent_id":    "main",
			"agents": map[string]interface{}{
				"reviewer": map[string]interface{}{
					"enabled":        true,
					"role":           "testing",
					"prompt":         "agents/reviewer/AGENT.md",
					"provider":       "openai",
					"tool_allowlist": []interface{}{"shell"},
					"runtime_class":  "provider_bound",
				},
			},
			"tools":   map[string]interface{}{"shell_enabled": true, "mcp_enabled": false},
			"gateway": map[string]interface{}{"host": "127.0.0.1", "port": float64(18790)},
		},
		"runtime": map[string]interface{}{
			"providers": map[string]interface{}{
				"openai": map[string]interface{}{
					"auth":        "bearer",
					"api_base":    "https://api.openai.com/v1",
					"timeout_sec": float64(30),
				},
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)
	srv.SetConfigAfterHook(func() error { return nil })

	req := httptest.NewRequest(http.MethodPost, "/api/config?mode=normalized", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	loaded, err := cfgpkg.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if _, ok := loaded.Agents.Agents["reviewer"]; !ok {
		t.Fatalf("expected reviewer agent, got %+v", loaded.Agents.Agents)
	}
}
