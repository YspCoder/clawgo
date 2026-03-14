package api

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/nodes"
)

func (s *Server) webUINodeArtifactsPayloadFiltered(nodeFilter, actionFilter, kindFilter string, limit int) []map[string]interface{} {
	nodeFilter = strings.TrimSpace(nodeFilter)
	actionFilter = strings.TrimSpace(actionFilter)
	kindFilter = strings.TrimSpace(kindFilter)
	rows, _ := s.readNodeDispatchAuditRows()
	if len(rows) == 0 {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, limit)
	for rowIndex := len(rows) - 1; rowIndex >= 0; rowIndex-- {
		row := rows[rowIndex]
		artifacts, _ := row["artifacts"].([]interface{})
		for artifactIndex, raw := range artifacts {
			artifact, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			item := map[string]interface{}{
				"id":             buildNodeArtifactID(row, artifact, artifactIndex),
				"time":           row["time"],
				"node":           row["node"],
				"action":         row["action"],
				"used_transport": row["used_transport"],
				"ok":             row["ok"],
				"error":          row["error"],
			}
			for _, key := range []string{"name", "kind", "mime_type", "storage", "path", "url", "content_text", "content_base64", "source_path", "size_bytes"} {
				if value, ok := artifact[key]; ok {
					item[key] = value
				}
			}
			if nodeFilter != "" && !strings.EqualFold(strings.TrimSpace(fmt.Sprint(item["node"])), nodeFilter) {
				continue
			}
			if actionFilter != "" && !strings.EqualFold(strings.TrimSpace(fmt.Sprint(item["action"])), actionFilter) {
				continue
			}
			if kindFilter != "" && !strings.EqualFold(strings.TrimSpace(fmt.Sprint(item["kind"])), kindFilter) {
				continue
			}
			out = append(out, item)
			if limit > 0 && len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func buildNodeArtifactID(row, artifact map[string]interface{}, artifactIndex int) string {
	seed := fmt.Sprintf("%v|%v|%v|%d|%v|%v|%v",
		row["time"], row["node"], row["action"], artifactIndex,
		artifact["name"], artifact["source_path"], artifact["path"],
	)
	sum := sha1.Sum([]byte(seed))
	return fmt.Sprintf("%x", sum[:8])
}

func sanitizeZipEntryName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "artifact.bin"
	}
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, name)
	if strings.Trim(name, "._") == "" {
		return "artifact.bin"
	}
	return name
}

func (s *Server) findNodeArtifactByID(id string) (map[string]interface{}, bool) {
	for _, item := range s.webUINodeArtifactsPayload(10000) {
		if strings.TrimSpace(fmt.Sprint(item["id"])) == id {
			return item, true
		}
	}
	return nil, false
}

func resolveArtifactPath(workspace, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if filepath.IsAbs(raw) {
		clean := filepath.Clean(raw)
		if info, err := os.Stat(clean); err == nil && !info.IsDir() {
			return clean
		}
		return ""
	}
	root := strings.TrimSpace(workspace)
	if root == "" {
		return ""
	}
	clean := filepath.Clean(filepath.Join(root, raw))
	if rel, err := filepath.Rel(root, clean); err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	if info, err := os.Stat(clean); err == nil && !info.IsDir() {
		return clean
	}
	return ""
}

func readArtifactBytes(workspace string, item map[string]interface{}) ([]byte, string, error) {
	if content := strings.TrimSpace(fmt.Sprint(item["content_base64"])); content != "" {
		raw, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return nil, "", err
		}
		return raw, strings.TrimSpace(fmt.Sprint(item["mime_type"])), nil
	}
	for _, rawPath := range []string{fmt.Sprint(item["source_path"]), fmt.Sprint(item["path"])} {
		if path := resolveArtifactPath(workspace, rawPath); path != "" {
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, "", err
			}
			return b, strings.TrimSpace(fmt.Sprint(item["mime_type"])), nil
		}
	}
	if contentText := fmt.Sprint(item["content_text"]); strings.TrimSpace(contentText) != "" {
		return []byte(contentText), "text/plain; charset=utf-8", nil
	}
	return nil, "", fmt.Errorf("artifact content unavailable")
}

func (s *Server) filteredNodeDispatches(nodeFilter, actionFilter string, limit int) []map[string]interface{} {
	items := s.webUINodesDispatchPayload(limit)
	if nodeFilter == "" && actionFilter == "" {
		return items
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if nodeFilter != "" && !strings.EqualFold(strings.TrimSpace(fmt.Sprint(item["node"])), nodeFilter) {
			continue
		}
		if actionFilter != "" && !strings.EqualFold(strings.TrimSpace(fmt.Sprint(item["action"])), actionFilter) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func filteredNodeAlerts(alerts []map[string]interface{}, nodeFilter string) []map[string]interface{} {
	if nodeFilter == "" {
		return alerts
	}
	out := make([]map[string]interface{}, 0, len(alerts))
	for _, item := range alerts {
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(item["node"])), nodeFilter) {
			out = append(out, item)
		}
	}
	return out
}

func (s *Server) handleWebUINodeArtifactsExport(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	retentionSummary := s.applyNodeArtifactRetention()
	limit := queryBoundedPositiveInt(r, "limit", 200, 1000)
	nodeFilter := strings.TrimSpace(r.URL.Query().Get("node"))
	actionFilter := strings.TrimSpace(r.URL.Query().Get("action"))
	kindFilter := strings.TrimSpace(r.URL.Query().Get("kind"))
	artifacts := s.webUINodeArtifactsPayloadFiltered(nodeFilter, actionFilter, kindFilter, limit)
	dispatches := s.filteredNodeDispatches(nodeFilter, actionFilter, limit)
	payload := s.webUINodesPayload(r.Context())
	nodeList, _ := payload["nodes"].([]nodes.NodeInfo)
	p2p, _ := payload["p2p"].(map[string]interface{})
	alerts := filteredNodeAlerts(s.webUINodeAlertsPayload(nodeList, p2p, dispatches), nodeFilter)

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	writeZipJSON := func(name string, value interface{}) error {
		entry, err := zw.Create(name)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(entry)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	}
	manifest := map[string]interface{}{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"filters": map[string]interface{}{
			"node":   nodeFilter,
			"action": actionFilter,
			"kind":   kindFilter,
			"limit":  limit,
		},
		"artifact_count": len(artifacts),
		"dispatch_count": len(dispatches),
		"alert_count":    len(alerts),
		"retention":      retentionSummary,
	}
	if err := writeZipJSON("manifest.json", manifest); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := writeZipJSON("dispatches.json", dispatches); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := writeZipJSON("alerts.json", alerts); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := writeZipJSON("artifacts.json", artifacts); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, item := range artifacts {
		name := sanitizeZipEntryName(firstNonEmptyString(
			fmt.Sprint(item["name"]),
			fmt.Sprint(item["source_path"]),
			fmt.Sprint(item["path"]),
			fmt.Sprintf("%s.bin", fmt.Sprint(item["id"])),
		))
		raw, _, err := readArtifactBytes(s.workspacePath, item)
		entryName := filepath.ToSlash(filepath.Join("files", fmt.Sprintf("%s-%s", fmt.Sprint(item["id"]), name)))
		if err != nil || len(raw) == 0 {
			entryName = filepath.ToSlash(filepath.Join("files", fmt.Sprintf("%s-metadata.json", fmt.Sprint(item["id"]))))
			raw, err = json.MarshalIndent(item, "", "  ")
			if err != nil {
				continue
			}
		}
		entry, err := zw.Create(entryName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := entry.Write(raw); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := zw.Close(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	filename := "node-artifacts-export.zip"
	if nodeFilter != "" {
		filename = fmt.Sprintf("node-artifacts-%s.zip", sanitizeZipEntryName(nodeFilter))
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive.Bytes())
}

func (s *Server) handleWebUINodeArtifactDownload(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	item, ok := s.findNodeArtifactByID(id)
	if !ok {
		http.Error(w, "artifact not found", http.StatusNotFound)
		return
	}
	name := strings.TrimSpace(fmt.Sprint(item["name"]))
	if name == "" {
		name = "artifact"
	}
	mimeType := strings.TrimSpace(fmt.Sprint(item["mime_type"]))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if contentB64 := strings.TrimSpace(fmt.Sprint(item["content_base64"])); contentB64 != "" {
		payload, err := base64.StdEncoding.DecodeString(contentB64)
		if err != nil {
			http.Error(w, "invalid inline artifact payload", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		_, _ = w.Write(payload)
		return
	}
	for _, rawPath := range []string{fmt.Sprint(item["source_path"]), fmt.Sprint(item["path"])} {
		if path := resolveArtifactPath(s.workspacePath, rawPath); path != "" {
			http.ServeFile(w, r, path)
			return
		}
	}
	if contentText := fmt.Sprint(item["content_text"]); strings.TrimSpace(contentText) != "" {
		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		_, _ = w.Write([]byte(contentText))
		return
	}
	http.Error(w, "artifact content unavailable", http.StatusNotFound)
}
