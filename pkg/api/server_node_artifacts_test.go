package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cfgpkg "github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/nodes"
)

func TestHandleWebUINodeArtifactsListAndDelete(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	workspace := t.TempDir()
	srv.SetWorkspacePath(workspace)
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	artifactPath := filepath.Join(workspace, "artifact.txt")
	if err := os.WriteFile(artifactPath, []byte("artifact-body"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	auditLine := "{\"time\":\"2026-03-09T00:00:00Z\",\"node\":\"edge-a\",\"action\":\"run\",\"artifacts\":[{\"name\":\"artifact.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"source_path\":\"artifact.txt\",\"size_bytes\":13}]}\n"
	if err := os.WriteFile(filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl"), []byte(auditLine), 0o644); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/node_artifacts", nil)
	listRec := httptest.NewRecorder()
	srv.handleWebUINodeArtifacts(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRec.Code)
	}
	var listBody map[string]interface{}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	items, _ := listBody["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("expected 1 artifact, got %+v", listBody)
	}
	item, _ := items[0].(map[string]interface{})
	artifactID := strings.TrimSpace(fmt.Sprint(item["id"]))
	if artifactID == "" {
		t.Fatalf("expected artifact id, got %+v", item)
	}

	deleteReq := httptest.NewRequest(http.MethodPost, "/api/node_artifacts/delete", strings.NewReader(fmt.Sprintf(`{"id":"%s"}`, artifactID)))
	deleteReq.Header.Set("Content-Type", "application/json")
	deleteRec := httptest.NewRecorder()
	srv.handleWebUINodeArtifactDelete(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}
	if _, err := os.Stat(artifactPath); !os.IsNotExist(err) {
		t.Fatalf("expected artifact file removed, stat err=%v", err)
	}
}

func TestHandleWebUINodeArtifactDownloadRejectsAbsoluteSourcePath(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	workspace := t.TempDir()
	srv.SetWorkspacePath(workspace)
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	artifactPath := filepath.Join(workspace, "artifact.txt")
	if err := os.WriteFile(artifactPath, []byte("artifact-body"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	auditLine := fmt.Sprintf("{\"time\":\"2026-03-09T00:00:00Z\",\"node\":\"edge-a\",\"action\":\"run\",\"artifacts\":[{\"name\":\"artifact.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"source_path\":\"%s\",\"size_bytes\":13}]}\n", artifactPath)
	if err := os.WriteFile(filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl"), []byte(auditLine), 0o644); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	items := srv.webUINodeArtifactsPayload(10)
	if len(items) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(items))
	}
	req := httptest.NewRequest(http.MethodGet, "/api/node_artifacts/download?id="+fmt.Sprint(items[0]["id"]), nil)
	rec := httptest.NewRecorder()
	srv.handleWebUINodeArtifactDownload(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleWebUINodeArtifactsExport(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	workspace := t.TempDir()
	srv.SetWorkspacePath(workspace)
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	auditLine := "{\"time\":\"2026-03-09T00:00:00Z\",\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"shot.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"Y2FwdHVyZQ==\",\"size_bytes\":7}]}\n"
	if err := os.WriteFile(filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl"), []byte(auditLine), 0o644); err != nil {
		t.Fatalf("write audit: %v", err)
	}
	srv.mgr.Upsert(nodes.NodeInfo{ID: "edge-a", Name: "Edge A", Online: true})

	req := httptest.NewRequest(http.MethodGet, "/api/node_artifacts/export?node=edge-a&action=screen_snapshot&kind=text", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUINodeArtifactsExport(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/zip") {
		t.Fatalf("expected zip response, got %q", got)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	seen := map[string]bool{}
	for _, file := range zr.File {
		seen[file.Name] = true
	}
	for _, required := range []string{"manifest.json", "dispatches.json", "alerts.json", "artifacts.json"} {
		if !seen[required] {
			t.Fatalf("missing zip entry %q in %+v", required, seen)
		}
	}
	foundFile := false
	for _, file := range zr.File {
		if !strings.HasPrefix(file.Name, "files/") {
			continue
		}
		foundFile = true
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open artifact file: %v", err)
		}
		body, _ := io.ReadAll(rc)
		_ = rc.Close()
		if string(body) != "capture" {
			t.Fatalf("unexpected artifact payload %q", string(body))
		}
	}
	if !foundFile {
		t.Fatalf("expected exported artifact file in zip")
	}
}

func TestHandleWebUINodeArtifactsPrune(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	workspace := t.TempDir()
	srv.SetWorkspacePath(workspace)
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	auditLines := strings.Join([]string{
		"{\"time\":\"2026-03-09T00:00:00Z\",\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"one.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"b25l\"}]}",
		"{\"time\":\"2026-03-09T00:01:00Z\",\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"two.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"dHdv\"}]}",
		"{\"time\":\"2026-03-09T00:02:00Z\",\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"three.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"dGhyZWU=\"}]}",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl"), []byte(auditLines), 0o644); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/node_artifacts/prune", strings.NewReader(`{"node":"edge-a","action":"screen_snapshot","kind":"text","keep_latest":1}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleWebUINodeArtifactPrune(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	items := srv.webUINodeArtifactsPayloadFiltered("edge-a", "screen_snapshot", "text", 10)
	if len(items) != 1 {
		t.Fatalf("expected 1 remaining artifact, got %d", len(items))
	}
	if got := fmt.Sprint(items[0]["name"]); got != "three.txt" {
		t.Fatalf("expected newest artifact to remain, got %q", got)
	}
}

func TestHandleWebUINodeArtifactsAppliesRetentionConfig(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	workspace := t.TempDir()
	srv.SetWorkspacePath(workspace)
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	cfg := cfgpkg.DefaultConfig()
	cfg.Gateway.Nodes.Artifacts.Enabled = true
	cfg.Gateway.Nodes.Artifacts.KeepLatest = 1
	cfg.Gateway.Nodes.Artifacts.PruneOnRead = true
	cfgPath := filepath.Join(workspace, "config.json")
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	srv.SetConfigPath(cfgPath)
	auditLines := strings.Join([]string{
		"{\"time\":\"2026-03-09T00:00:00Z\",\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"one.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"b25l\"}]}",
		"{\"time\":\"2026-03-09T00:01:00Z\",\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"two.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"dHdv\"}]}",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl"), []byte(auditLines), 0o644); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/node_artifacts", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUINodeArtifacts(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	items := srv.webUINodeArtifactsPayload(10)
	if len(items) != 1 {
		t.Fatalf("expected retention to keep 1 artifact, got %d", len(items))
	}
	if got := fmt.Sprint(items[0]["name"]); got != "two.txt" {
		t.Fatalf("expected newest artifact to remain, got %q", got)
	}
	stats := srv.artifactStatsSnapshot()
	if fmt.Sprint(stats["pruned"]) == "" || fmt.Sprint(stats["pruned"]) == "0" {
		t.Fatalf("expected retention stats to record pruned artifacts, got %+v", stats)
	}
	if fmt.Sprint(stats["keep_latest"]) != "1" {
		t.Fatalf("expected keep_latest in stats, got %+v", stats)
	}
}

func TestHandleWebUINodeArtifactsAppliesRetentionDays(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	workspace := t.TempDir()
	srv.SetWorkspacePath(workspace)
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	cfg := cfgpkg.DefaultConfig()
	cfg.Gateway.Nodes.Artifacts.Enabled = true
	cfg.Gateway.Nodes.Artifacts.KeepLatest = 10
	cfg.Gateway.Nodes.Artifacts.RetainDays = 1
	cfg.Gateway.Nodes.Artifacts.PruneOnRead = true
	cfgPath := filepath.Join(workspace, "config.json")
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	srv.SetConfigPath(cfgPath)
	oldTime := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	newTime := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	auditLines := strings.Join([]string{
		fmt.Sprintf("{\"time\":%q,\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"old.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"b2xk\"}]}", oldTime),
		fmt.Sprintf("{\"time\":%q,\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"fresh.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"ZnJlc2g=\"}]}", newTime),
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl"), []byte(auditLines), 0o644); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/node_artifacts", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUINodeArtifacts(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	items := srv.webUINodeArtifactsPayload(10)
	if len(items) != 1 {
		t.Fatalf("expected retention days to keep 1 artifact, got %d", len(items))
	}
	if got := fmt.Sprint(items[0]["name"]); got != "fresh.txt" {
		t.Fatalf("expected fresh artifact to remain, got %q", got)
	}
}
