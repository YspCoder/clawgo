package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	rpcpkg "github.com/YspCoder/clawgo/pkg/rpc"
	"github.com/YspCoder/clawgo/pkg/tools"
)

func (s *Server) handleWebUIConfig(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if strings.TrimSpace(s.configPath) == "" {
		http.Error(w, "config path not set", http.StatusInternalServerError)
		return
	}
	svc := s.configRPCService()
	switch r.Method {
	case http.MethodGet:
		mode := strings.TrimSpace(r.URL.Query().Get("mode"))
		includeHot := r.URL.Query().Get("include_hot_reload_fields") == "1" || strings.EqualFold(mode, "hot")
		resp, rpcErr := svc.View(r.Context(), rpcpkg.ConfigViewRequest{
			Mode:                 mode,
			IncludeHotReloadInfo: includeHot,
		})
		if rpcErr != nil {
			http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
			return
		}
		if strings.EqualFold(mode, "normalized") || includeHot {
			payload := map[string]interface{}{"ok": true, "config": resp.Config}
			if resp.RawConfig != nil {
				payload["raw_config"] = resp.RawConfig
			}
			if len(resp.HotReloadFields) > 0 {
				payload["hot_reload_fields"] = resp.HotReloadFields
				payload["hot_reload_field_details"] = resp.HotReloadFieldDetails
			}
			writeJSON(w, payload)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp.PrettyText))
	case http.MethodPost:
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		confirmRisky, _ := tools.MapBoolArg(body, "confirm_risky")
		delete(body, "confirm_risky")
		resp, rpcErr := svc.Save(r.Context(), rpcpkg.ConfigSaveRequest{
			Mode:         strings.TrimSpace(r.URL.Query().Get("mode")),
			ConfirmRisky: confirmRisky,
			Config:       body,
		})
		if rpcErr != nil {
			message := rpcErr.Message
			status := rpcHTTPStatus(rpcErr)
			if status == http.StatusInternalServerError && strings.TrimSpace(message) != "" && !strings.Contains(strings.ToLower(message), "reload failed") {
				message = "config saved but reload failed: " + message
			}
			payload := map[string]interface{}{"ok": false, "error": message}
			if resp != nil && resp.RequiresConfirm {
				payload["requires_confirm"] = true
				payload["changed_fields"] = resp.ChangedFields
			}
			if resp != nil && resp.Details != nil {
				payload["details"] = resp.Details
			}
			writeJSONStatus(w, status, payload)
			return
		}
		out := map[string]interface{}{"ok": true, "reloaded": true}
		if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("mode")), "normalized") {
			view, viewErr := svc.View(r.Context(), rpcpkg.ConfigViewRequest{Mode: "normalized"})
			if viewErr == nil {
				out["config"] = view.Config
			}
		}
		writeJSON(w, out)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebUIProviderModels(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Provider string   `json:"provider"`
		Model    string   `json:"model"`
		Models   []string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	resp, rpcErr := s.providerRPCService().UpdateModels(r.Context(), rpcpkg.UpdateProviderModelsRequest{
		Provider: strings.TrimSpace(body.Provider),
		Model:    body.Model,
		Models:   body.Models,
	})
	if rpcErr != nil {
		http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":     true,
		"models": resp.Models,
	})
}

func (s *Server) handleWebUIProviderRuntime(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		resp, rpcErr := s.providerRPCService().RuntimeView(r.Context(), rpcpkg.ProviderRuntimeViewRequest{
			Provider:               strings.TrimSpace(r.URL.Query().Get("provider")),
			Kind:                   strings.TrimSpace(r.URL.Query().Get("kind")),
			Reason:                 strings.TrimSpace(r.URL.Query().Get("reason")),
			Target:                 strings.TrimSpace(r.URL.Query().Get("target")),
			Sort:                   strings.TrimSpace(r.URL.Query().Get("sort")),
			ChangesOnly:            strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("changes_only")), "true"),
			WindowSec:              atoiDefault(strings.TrimSpace(r.URL.Query().Get("window_sec")), 0),
			Limit:                  atoiDefault(strings.TrimSpace(r.URL.Query().Get("limit")), 0),
			Cursor:                 atoiDefault(strings.TrimSpace(r.URL.Query().Get("cursor")), 0),
			HealthBelow:            atoiDefault(strings.TrimSpace(r.URL.Query().Get("health_below")), 0),
			CooldownUntilBeforeSec: atoiDefault(strings.TrimSpace(r.URL.Query().Get("cooldown_until_before_sec")), 0),
		})
		if rpcErr != nil {
			http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
			return
		}
		writeJSON(w, map[string]interface{}{
			"ok":   true,
			"view": resp.View,
		})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Provider     string `json:"provider"`
		Action       string `json:"action"`
		OnlyExpiring bool   `json:"only_expiring"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	resp, rpcErr := s.providerRPCService().RuntimeAction(r.Context(), rpcpkg.ProviderRuntimeActionRequest{
		Provider:     strings.TrimSpace(body.Provider),
		Action:       strings.TrimSpace(body.Action),
		OnlyExpiring: body.OnlyExpiring,
	})
	if rpcErr != nil {
		http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
		return
	}
	result := map[string]interface{}{"ok": true}
	for key, value := range resp.Result {
		result[key] = value
	}
	writeJSON(w, result)
}

func (s *Server) handleWebUINodeDispatchReplay(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Node   string                 `json:"node"`
		Action string                 `json:"action"`
		Mode   string                 `json:"mode"`
		Task   string                 `json:"task"`
		Model  string                 `json:"model"`
		Args   map[string]interface{} `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	resp, rpcErr := s.nodeRPCService().Dispatch(r.Context(), rpcpkg.DispatchNodeRequest{
		Node:   strings.TrimSpace(body.Node),
		Action: strings.TrimSpace(body.Action),
		Mode:   strings.TrimSpace(body.Mode),
		Task:   body.Task,
		Model:  body.Model,
		Args:   body.Args,
	})
	if rpcErr != nil {
		http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":     true,
		"result": resp.Result,
	})
}

func (s *Server) handleWebUINodeArtifacts(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp, rpcErr := s.nodeRPCService().ListArtifacts(r.Context(), rpcpkg.ListNodeArtifactsRequest{
		Node:   strings.TrimSpace(r.URL.Query().Get("node")),
		Action: strings.TrimSpace(r.URL.Query().Get("action")),
		Kind:   strings.TrimSpace(r.URL.Query().Get("kind")),
		Limit:  queryBoundedPositiveInt(r, "limit", 200, 1000),
	})
	if rpcErr != nil {
		http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":                 true,
		"items":              resp.Items,
		"artifact_retention": resp.ArtifactRetention,
	})
}

func (s *Server) handleWebUINodeArtifactDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	resp, rpcErr := s.nodeRPCService().DeleteArtifact(r.Context(), rpcpkg.DeleteNodeArtifactRequest{ID: strings.TrimSpace(body.ID)})
	if rpcErr != nil {
		http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":            true,
		"id":            resp.ID,
		"deleted_file":  resp.DeletedFile,
		"deleted_audit": resp.DeletedAudit,
	})
}

func (s *Server) handleWebUINodeArtifactPrune(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Node       string `json:"node"`
		Action     string `json:"action"`
		Kind       string `json:"kind"`
		KeepLatest int    `json:"keep_latest"`
		Limit      int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	resp, rpcErr := s.nodeRPCService().PruneArtifacts(r.Context(), rpcpkg.PruneNodeArtifactsRequest{
		Node:       strings.TrimSpace(body.Node),
		Action:     strings.TrimSpace(body.Action),
		Kind:       strings.TrimSpace(body.Kind),
		KeepLatest: body.KeepLatest,
		Limit:      body.Limit,
	})
	if rpcErr != nil {
		http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":            true,
		"pruned":        resp.Pruned,
		"deleted_files": resp.DeletedFiles,
		"kept":          resp.Kept,
	})
}

func (s *Server) handleWebUICron(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.onCron == nil {
		http.Error(w, "cron handler not configured", http.StatusInternalServerError)
		return
	}
	svc := s.cronRPCService()
	switch r.Method {
	case http.MethodGet:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			resp, rpcErr := svc.List(r.Context(), rpcpkg.ListCronJobsRequest{})
			if rpcErr != nil {
				http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "jobs": resp.Jobs})
		} else {
			resp, rpcErr := svc.Get(r.Context(), rpcpkg.GetCronJobRequest{ID: id})
			if rpcErr != nil {
				http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "job": resp.Job})
		}
	case http.MethodPost:
		args := map[string]interface{}{}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&args)
		}
		if id := strings.TrimSpace(r.URL.Query().Get("id")); id != "" {
			args["id"] = id
		}
		action := "create"
		if a := tools.MapStringArg(args, "action"); a != "" {
			action = strings.ToLower(strings.TrimSpace(a))
		}
		resp, rpcErr := svc.Mutate(r.Context(), rpcpkg.MutateCronJobRequest{
			Action: action,
			Args:   args,
		})
		if rpcErr != nil {
			http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "result": resp.Result})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebUISubagentsRuntime(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.onSubagents == nil {
		http.Error(w, "subagent runtime handler not configured", http.StatusServiceUnavailable)
		return
	}

	action := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("action")))
	args := map[string]interface{}{}
	switch r.Method {
	case http.MethodGet:
		if action == "" {
			action = "list"
		}
		for key, values := range r.URL.Query() {
			if key == "action" || key == "token" || len(values) == 0 {
				continue
			}
			args[key] = strings.TrimSpace(values[0])
		}
	case http.MethodPost:
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if body == nil {
			body = map[string]interface{}{}
		}
		if action == "" {
			if raw := stringFromMap(body, "action"); raw != "" {
				action = strings.ToLower(strings.TrimSpace(raw))
			}
		}
		delete(body, "action")
		args = body
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, rpcErr := s.handleSubagentLegacyAction(r.Context(), action, args)
	if rpcErr != nil {
		http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "result": result})
}

func (s *Server) handleWebUIMemory(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	svc := s.workspaceRPCService()
	switch r.Method {
	case http.MethodGet:
		path := strings.TrimSpace(r.URL.Query().Get("path"))
		if path == "" {
			resp, rpcErr := svc.ListFiles(r.Context(), rpcpkg.ListWorkspaceFilesRequest{Scope: "memory"})
			if rpcErr != nil {
				http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "files": resp.Files})
			return
		}
		resp, rpcErr := svc.ReadFile(r.Context(), rpcpkg.ReadWorkspaceFileRequest{Scope: "memory", Path: path})
		if rpcErr != nil {
			http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
			return
		}
		if !resp.Found {
			http.Error(w, os.ErrNotExist.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "path": resp.Path, "content": resp.Content})
	case http.MethodPost:
		var body struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		resp, rpcErr := svc.WriteFile(r.Context(), rpcpkg.WriteWorkspaceFileRequest{Scope: "memory", Path: body.Path, Content: body.Content})
		if rpcErr != nil {
			http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "path": resp.Path})
	case http.MethodDelete:
		resp, rpcErr := svc.DeleteFile(r.Context(), rpcpkg.DeleteWorkspaceFileRequest{Scope: "memory", Path: r.URL.Query().Get("path")})
		if rpcErr != nil {
			http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "deleted": resp.Deleted, "path": resp.Path})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebUIWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	svc := s.workspaceRPCService()
	switch r.Method {
	case http.MethodGet:
		path := strings.TrimSpace(r.URL.Query().Get("path"))
		if path == "" {
			resp, rpcErr := svc.ListFiles(r.Context(), rpcpkg.ListWorkspaceFilesRequest{Scope: "workspace"})
			if rpcErr != nil {
				http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "files": resp.Files})
			return
		}
		resp, rpcErr := svc.ReadFile(r.Context(), rpcpkg.ReadWorkspaceFileRequest{Scope: "workspace", Path: path})
		if rpcErr != nil {
			http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "path": resp.Path, "found": resp.Found, "content": resp.Content})
	case http.MethodPost:
		var body struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		resp, rpcErr := svc.WriteFile(r.Context(), rpcpkg.WriteWorkspaceFileRequest{Scope: "workspace", Path: body.Path, Content: body.Content})
		if rpcErr != nil {
			http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "path": resp.Path, "saved": resp.Saved})
	case http.MethodDelete:
		resp, rpcErr := svc.DeleteFile(r.Context(), rpcpkg.DeleteWorkspaceFileRequest{Scope: "workspace", Path: r.URL.Query().Get("path")})
		if rpcErr != nil {
			http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "deleted": resp.Deleted, "path": resp.Path})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
