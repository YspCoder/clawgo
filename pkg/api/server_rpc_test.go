package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfgpkg "github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/nodes"
)

func TestHandleNodeRPCDispatch(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	srv.SetNodeDispatchHandler(func(ctx context.Context, req nodes.Request, mode string) (nodes.Response, error) {
		if req.Node != "edge-a" || req.Action != "screen_snapshot" || mode != "relay" {
			t.Fatalf("unexpected request: %+v mode=%s", req, mode)
		}
		return nodes.Response{
			OK:     true,
			Node:   req.Node,
			Action: req.Action,
			Payload: map[string]interface{}{
				"used_transport": "relay",
			},
		}, nil
	})

	body := `{"method":"node.dispatch","request_id":"req-2","params":{"node":"edge-a","action":"screen_snapshot","mode":"relay","args":{"quality":"high"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/rpc/node", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleNodeRPC(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"request_id":"req-2"`) || !strings.Contains(rec.Body.String(), `"used_transport":"relay"`) {
		t.Fatalf("unexpected rpc body: %s", rec.Body.String())
	}
}

func TestHandleProviderRPCListModels(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	pc := cfg.Models.Providers["openai"]
	pc.APIBase = "https://example.invalid/v1"
	pc.APIKey = "test-key"
	pc.Models = []string{"gpt-5.4", "gpt-5.4-mini"}
	cfg.Models.Providers["openai"] = pc
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	srv.SetConfigPath(cfgPath)

	body := `{"method":"provider.list_models","request_id":"req-p1","params":{"provider":"openai"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/rpc/provider", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleProviderRPC(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"gpt-5.4"`) || !strings.Contains(rec.Body.String(), `"request_id":"req-p1"`) {
		t.Fatalf("unexpected provider rpc body: %s", rec.Body.String())
	}
}

func TestHandleProviderRPCCountTokensUnavailable(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	pc := cfg.Models.Providers["openai"]
	pc.APIBase = "https://example.invalid/v1"
	pc.APIKey = "test-key"
	pc.Models = []string{"gpt-5.4"}
	cfg.Models.Providers["openai"] = pc
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	srv.SetConfigPath(cfgPath)

	body := `{"method":"provider.count_tokens","request_id":"req-p2","params":{"provider":"openai","messages":[{"role":"user","content":"hello"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/rpc/provider", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleProviderRPC(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"unavailable"`) {
		t.Fatalf("expected unavailable rpc error, got: %s", rec.Body.String())
	}
}

func TestHandleSkillsRPCView(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	skillsDir := filepath.Join(workspace, "skills", "demo")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte(buildSkillMarkdown("demo", "Demo skill", []string{"shell"}, "Be useful")), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	srv.SetWorkspacePath(workspace)

	body := `{"method":"skills.view","request_id":"req-s1","params":{"id":"demo","files":true}}`
	req := httptest.NewRequest(http.MethodPost, "/api/rpc/skills", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleSkillsRPC(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"request_id":"req-s1"`) || !strings.Contains(rec.Body.String(), `SKILL.md`) {
		t.Fatalf("unexpected skills rpc body: %s", rec.Body.String())
	}
}

func TestHandleWebUISkillsUsesRPCFacade(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	skillsDir := filepath.Join(workspace, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	srv.SetWorkspacePath(workspace)

	req := httptest.NewRequest(http.MethodPost, "/api/skills", strings.NewReader(`{"action":"create","name":"demo","description":"Demo skill","system_prompt":"Be useful","tools":["shell"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleWebUISkills(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "demo", "SKILL.md")); err != nil {
		t.Fatalf("expected created skill file: %v", err)
	}
}

func TestHandleWebUIProviderModelsUsesRPCFacade(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	pc := cfg.Models.Providers["openai"]
	pc.APIBase = "https://example.invalid/v1"
	pc.APIKey = "test-key"
	pc.Models = []string{"gpt-5.4-mini"}
	cfg.Models.Providers["openai"] = pc
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	srv.SetConfigPath(cfgPath)
	srv.SetConfigAfterHook(func() error { return nil })

	req := httptest.NewRequest(http.MethodPost, "/api/provider/models", strings.NewReader(`{"provider":"openai","model":"gpt-5.4"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleWebUIProviderModels(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"gpt-5.4"`) {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
}

func TestHandleWebUIProviderRuntimeUsesRPCFacade(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	pc := cfg.Models.Providers["openai"]
	pc.APIBase = "https://example.invalid/v1"
	pc.APIKey = "test-key"
	pc.Models = []string{"gpt-5.4-mini"}
	cfg.Models.Providers["openai"] = pc
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	srv.SetConfigPath(cfgPath)

	req := httptest.NewRequest(http.MethodGet, "/api/provider/runtime?provider=openai&limit=5", nil)
	rec := httptest.NewRecorder()

	srv.handleWebUIProviderRuntime(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"view"`) {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
}
