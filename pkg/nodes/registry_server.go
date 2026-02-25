package nodes

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type RegistryServer struct {
	addr          string
	token         string
	mgr           *Manager
	server        *http.Server
	configPath    string
	workspacePath string
	logFilePath   string
	onChat        func(ctx context.Context, sessionKey, content string) (string, error)
	onConfigAfter func()
	onCron        func(action string, args map[string]interface{}) (interface{}, error)
	webUIDir      string
}

func NewRegistryServer(host string, port int, token string, mgr *Manager) *RegistryServer {
	addr := strings.TrimSpace(host)
	if addr == "" {
		addr = "0.0.0.0"
	}
	if port <= 0 {
		port = 7788
	}
	return &RegistryServer{addr: fmt.Sprintf("%s:%d", addr, port), token: strings.TrimSpace(token), mgr: mgr}
}

func (s *RegistryServer) SetConfigPath(path string) { s.configPath = strings.TrimSpace(path) }
func (s *RegistryServer) SetWorkspacePath(path string) { s.workspacePath = strings.TrimSpace(path) }
func (s *RegistryServer) SetLogFilePath(path string) { s.logFilePath = strings.TrimSpace(path) }
func (s *RegistryServer) SetChatHandler(fn func(ctx context.Context, sessionKey, content string) (string, error)) {
	s.onChat = fn
}
func (s *RegistryServer) SetConfigAfterHook(fn func()) { s.onConfigAfter = fn }
func (s *RegistryServer) SetCronHandler(fn func(action string, args map[string]interface{}) (interface{}, error)) {
	s.onCron = fn
}
func (s *RegistryServer) SetWebUIDir(dir string) { s.webUIDir = strings.TrimSpace(dir) }

func (s *RegistryServer) Start(ctx context.Context) error {
	if s.mgr == nil {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/nodes/register", s.handleRegister)
	mux.HandleFunc("/nodes/heartbeat", s.handleHeartbeat)
	mux.HandleFunc("/webui", s.handleWebUI)
	mux.HandleFunc("/webui/", s.handleWebUIAsset)
	mux.HandleFunc("/webui/api/config", s.handleWebUIConfig)
	mux.HandleFunc("/webui/api/chat", s.handleWebUIChat)
	mux.HandleFunc("/webui/api/chat/stream", s.handleWebUIChatStream)
	mux.HandleFunc("/webui/api/upload", s.handleWebUIUpload)
	mux.HandleFunc("/webui/api/nodes", s.handleWebUINodes)
	mux.HandleFunc("/webui/api/cron", s.handleWebUICron)
	mux.HandleFunc("/webui/api/skills", s.handleWebUISkills)
	mux.HandleFunc("/webui/api/exec_approvals", s.handleWebUIExecApprovals)
	mux.HandleFunc("/webui/api/logs/stream", s.handleWebUILogsStream)
	s.server = &http.Server{Addr: s.addr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
	}()
	go func() { _ = s.server.ListenAndServe() }()
	return nil
}

func (s *RegistryServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var n NodeInfo
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(n.ID) == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	s.mgr.Upsert(n)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": n.ID})
}

func (s *RegistryServer) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct{ ID string `json:"id"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	n, ok := s.mgr.Get(body.ID)
	if !ok {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	n.LastSeenAt = time.Now().UTC()
	n.Online = true
	s.mgr.Upsert(n)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": body.ID})
}

func (s *RegistryServer) handleWebUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.token != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     "clawgo_webui_token",
			Value:    s.token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   86400,
		})
	}
	if s.tryServeWebUIDist(w, r, "/webui/index.html") {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(webUIHTML))
}

func (s *RegistryServer) handleWebUIAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.tryServeWebUIDist(w, r, r.URL.Path) {
		return
	}
	// SPA fallback
	if s.tryServeWebUIDist(w, r, "/webui/index.html") {
		return
	}
	http.NotFound(w, r)
}

func (s *RegistryServer) tryServeWebUIDist(w http.ResponseWriter, r *http.Request, reqPath string) bool {
	dir := strings.TrimSpace(s.webUIDir)
	if dir == "" {
		return false
	}
	p := strings.TrimPrefix(reqPath, "/webui/")
	if reqPath == "/webui" || reqPath == "/webui/" || reqPath == "/webui/index.html" {
		p = "index.html"
	}
	p = filepath.Clean(strings.TrimPrefix(p, "/"))
	if strings.HasPrefix(p, "..") {
		return false
	}
	full := filepath.Join(dir, p)
	fi, err := os.Stat(full)
	if err != nil || fi.IsDir() {
		return false
	}
	http.ServeFile(w, r, full)
	return true
}

func (s *RegistryServer) handleWebUIConfig(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if strings.TrimSpace(s.configPath) == "" {
		http.Error(w, "config path not set", http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		b, err := os.ReadFile(s.configPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if r.URL.Query().Get("include_hot_reload_fields") == "1" || strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("mode")), "hot") {
			var cfg map[string]interface{}
			if err := json.Unmarshal(b, &cfg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":                true,
				"config":            cfg,
				"hot_reload_fields": hotReloadFieldPaths(),
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	case http.MethodPost:
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		b, err := json.MarshalIndent(body, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		tmp := s.configPath + ".tmp"
		if err := os.WriteFile(tmp, b, 0644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.Rename(tmp, s.configPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if s.onConfigAfter != nil {
			s.onConfigAfter()
		} else {
			_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "reloaded": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *RegistryServer) handleWebUIUpload(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", http.StatusBadRequest)
		return
	}
	defer f.Close()
	dir := filepath.Join(os.TempDir(), "clawgo_webui_uploads")
	_ = os.MkdirAll(dir, 0755)
	name := fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(h.Filename))
	path := filepath.Join(dir, name)
	out, err := os.Create(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, f); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "path": path, "name": h.Filename})
}

func (s *RegistryServer) handleWebUIChat(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.onChat == nil {
		http.Error(w, "chat handler not configured", http.StatusInternalServerError)
		return
	}
	var body struct {
		Session string `json:"session"`
		Message string `json:"message"`
		Media   string `json:"media"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	session := strings.TrimSpace(body.Session)
	if session == "" {
		session = "webui:default"
	}
	prompt := strings.TrimSpace(body.Message)
	if strings.TrimSpace(body.Media) != "" {
		if prompt != "" {
			prompt += "\n"
		}
		prompt += "[file: " + strings.TrimSpace(body.Media) + "]"
	}
	resp, err := s.onChat(r.Context(), session, prompt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "reply": resp, "session": session})
}

func (s *RegistryServer) handleWebUIChatStream(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.onChat == nil {
		http.Error(w, "chat handler not configured", http.StatusInternalServerError)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	var body struct {
		Session string `json:"session"`
		Message string `json:"message"`
		Media   string `json:"media"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	session := strings.TrimSpace(body.Session)
	if session == "" {
		session = "webui:default"
	}
	prompt := strings.TrimSpace(body.Message)
	if strings.TrimSpace(body.Media) != "" {
		if prompt != "" {
			prompt += "\n"
		}
		prompt += "[file: " + strings.TrimSpace(body.Media) + "]"
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	resp, err := s.onChat(r.Context(), session, prompt)
	if err != nil {
		_, _ = w.Write([]byte("Error: " + err.Error()))
		flusher.Flush()
		return
	}
	chunk := 180
	for i := 0; i < len(resp); i += chunk {
		end := i + chunk
		if end > len(resp) {
			end = len(resp)
		}
		_, _ = w.Write([]byte(resp[i:end]))
		flusher.Flush()
	}
}

func (s *RegistryServer) handleWebUINodes(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		list := []NodeInfo{}
		if s.mgr != nil {
			list = s.mgr.List()
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "nodes": list})
	case http.MethodPost:
		var body struct {
			Action string `json:"action"`
			ID     string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action := strings.ToLower(strings.TrimSpace(body.Action))
		if action != "delete" {
			http.Error(w, "unsupported action", http.StatusBadRequest)
			return
		}
		if s.mgr == nil {
			http.Error(w, "nodes manager unavailable", http.StatusInternalServerError)
			return
		}
		id := strings.TrimSpace(body.ID)
		ok := s.mgr.Remove(id)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "deleted": ok, "id": id})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *RegistryServer) handleWebUICron(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.onCron == nil {
		http.Error(w, "cron handler not configured", http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		action := "list"
		if id != "" {
			action = "get"
		}
		res, err := s.onCron(action, map[string]interface{}{"id": id})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if action == "list" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "jobs": normalizeCronJobs(res)})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "job": normalizeCronJob(res)})
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
		if a, ok := args["action"].(string); ok && strings.TrimSpace(a) != "" {
			action = strings.ToLower(strings.TrimSpace(a))
		}
		res, err := s.onCron(action, args)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "result": normalizeCronJob(res)})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *RegistryServer) handleWebUISkills(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	skillsDir := filepath.Join(strings.TrimSpace(s.workspacePath), "skills")
	if strings.TrimSpace(skillsDir) == "" {
		http.Error(w, "workspace not configured", http.StatusInternalServerError)
		return
	}
	_ = os.MkdirAll(skillsDir, 0755)

	switch r.Method {
	case http.MethodGet:
		entries, err := os.ReadDir(skillsDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type skillItem struct {
			ID            string   `json:"id"`
			Name          string   `json:"name"`
			Description   string   `json:"description"`
			Tools         []string `json:"tools"`
			SystemPrompt  string   `json:"system_prompt,omitempty"`
			Enabled       bool     `json:"enabled"`
			UpdateChecked bool     `json:"update_checked"`
			RemoteFound   bool     `json:"remote_found,omitempty"`
			RemoteVersion string   `json:"remote_version,omitempty"`
			CheckError    string   `json:"check_error,omitempty"`
		}
		items := make([]skillItem, 0, len(entries))
		checkUpdates := strings.TrimSpace(r.URL.Query().Get("check_updates")) != "0"
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			enabled := !strings.HasSuffix(name, ".disabled")
			baseName := strings.TrimSuffix(name, ".disabled")
			desc, tools, sys := readSkillMeta(filepath.Join(skillsDir, name, "SKILL.md"))
			if desc == "" || len(tools) == 0 || sys == "" {
				d2, t2, s2 := readSkillMeta(filepath.Join(skillsDir, baseName, "SKILL.md"))
				if desc == "" { desc = d2 }
				if len(tools) == 0 { tools = t2 }
				if sys == "" { sys = s2 }
			}
			it := skillItem{ID: baseName, Name: baseName, Description: desc, Tools: tools, SystemPrompt: sys, Enabled: enabled, UpdateChecked: checkUpdates}
			if checkUpdates {
				found, version, checkErr := queryClawHubSkillVersion(r.Context(), baseName)
				it.RemoteFound = found
				it.RemoteVersion = version
				if checkErr != nil {
					it.CheckError = checkErr.Error()
				}
			}
			items = append(items, it)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "skills": items, "source": "clawhub"})
	case http.MethodPost:
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action, _ := body["action"].(string)
		action = strings.ToLower(strings.TrimSpace(action))
		id, _ := body["id"].(string)
		name, _ := body["name"].(string)
		if strings.TrimSpace(name) == "" {
			name = id
		}
		name = strings.TrimSpace(name)
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		enabledPath := filepath.Join(skillsDir, name)
		disabledPath := enabledPath + ".disabled"

		switch action {
		case "enable":
			if _, err := os.Stat(disabledPath); err == nil {
				if err := os.Rename(disabledPath, enabledPath); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		case "disable":
			if _, err := os.Stat(enabledPath); err == nil {
				if err := os.Rename(enabledPath, disabledPath); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		case "create", "update":
			desc, _ := body["description"].(string)
			sys, _ := body["system_prompt"].(string)
			var toolsList []string
			if arr, ok := body["tools"].([]interface{}); ok {
				for _, v := range arr {
					if sv, ok := v.(string); ok && strings.TrimSpace(sv) != "" {
						toolsList = append(toolsList, strings.TrimSpace(sv))
					}
				}
			}
			if action == "create" {
				if _, err := os.Stat(enabledPath); err == nil {
					http.Error(w, "skill already exists", http.StatusBadRequest)
					return
				}
			}
			if err := os.MkdirAll(filepath.Join(enabledPath, "scripts"), 0755); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			skillMD := buildSkillMarkdown(name, desc, toolsList, sys)
			if err := os.WriteFile(filepath.Join(enabledPath, "SKILL.md"), []byte(skillMD), 0644); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		default:
			http.Error(w, "unsupported action", http.StatusBadRequest)
		}
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		pathA := filepath.Join(skillsDir, id)
		pathB := pathA + ".disabled"
		deleted := false
		if err := os.RemoveAll(pathA); err == nil {
			deleted = true
		}
		if err := os.RemoveAll(pathB); err == nil {
			deleted = true
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "deleted": deleted, "id": id})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func buildSkillMarkdown(name, desc string, tools []string, systemPrompt string) string {
	if strings.TrimSpace(desc) == "" {
		desc = "No description provided."
	}
	t := strings.Join(tools, ", ")
	return fmt.Sprintf(`---
name: %s
description: %s
---

# %s

%s

## Tools
%s

## System Prompt
%s
`, name, desc, name, desc, t, systemPrompt)
}

func readSkillMeta(path string) (desc string, tools []string, systemPrompt string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil, ""
	}
	s := string(b)
	reDesc := regexp.MustCompile(`(?m)^description:\s*(.+)$`)
	reTools := regexp.MustCompile(`(?m)^##\s*Tools\s*$`)
	rePrompt := regexp.MustCompile(`(?m)^##\s*System Prompt\s*$`)
	if m := reDesc.FindStringSubmatch(s); len(m) > 1 {
		desc = strings.TrimSpace(m[1])
	}
	if loc := reTools.FindStringIndex(s); loc != nil {
		block := s[loc[1]:]
		if p := rePrompt.FindStringIndex(block); p != nil {
			block = block[:p[0]]
		}
		for _, line := range strings.Split(block, "\n") {
			v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
			if v != "" {
				tools = append(tools, v)
			}
		}
	}
	if loc := rePrompt.FindStringIndex(s); loc != nil {
		systemPrompt = strings.TrimSpace(s[loc[1]:])
	}
	return
}


func normalizeCronJob(v interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]interface{}{"raw": fmt.Sprintf("%v", v)}
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]interface{}{"raw": string(b)}
	}
	out := map[string]interface{}{}
	for k, val := range m {
		out[k] = val
	}
	if sch, ok := m["schedule"].(map[string]interface{}); ok {
		if kind, ok := sch["kind"]; ok { out["kind"] = kind }
		if every, ok := sch["everyMs"]; ok { out["everyMs"] = every }
		if expr, ok := sch["expr"]; ok { out["expr"] = expr }
		if at, ok := sch["atMs"]; ok { out["atMs"] = at }
	}
	if payload, ok := m["payload"].(map[string]interface{}); ok {
		if msg, ok := payload["message"]; ok { out["message"] = msg }
		if d, ok := payload["deliver"]; ok { out["deliver"] = d }
		if c, ok := payload["channel"]; ok { out["channel"] = c }
		if to, ok := payload["to"]; ok { out["to"] = to }
	}
	return out
}

func normalizeCronJobs(v interface{}) []map[string]interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		return []map[string]interface{}{}
	}
	var arr []interface{}
	if err := json.Unmarshal(b, &arr); err != nil {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, it := range arr {
		out = append(out, normalizeCronJob(it))
	}
	return out
}

func queryClawHubSkillVersion(ctx context.Context, skill string) (found bool, version string, err error) {
	skill = strings.TrimSpace(skill)
	if skill == "" {
		return false, "", fmt.Errorf("skill empty")
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "clawhub", "search", skill, "--json")
	out, runErr := cmd.Output()
	if runErr != nil {
		return false, "", runErr
	}
	var payload interface{}
	if err := json.Unmarshal(out, &payload); err != nil {
		return false, "", err
	}
	lowerSkill := strings.ToLower(skill)
	var walk func(v interface{}) (bool, string)
	walk = func(v interface{}) (bool, string) {
		switch t := v.(type) {
		case map[string]interface{}:
			name := strings.ToLower(strings.TrimSpace(anyToString(t["name"])))
			if name == "" {
				name = strings.ToLower(strings.TrimSpace(anyToString(t["id"])))
			}
			if name == lowerSkill || strings.Contains(name, lowerSkill) {
				ver := strings.TrimSpace(anyToString(t["version"]))
				if ver == "" {
					ver = strings.TrimSpace(anyToString(t["latest_version"]))
				}
				return true, ver
			}
			for _, vv := range t {
				if ok, ver := walk(vv); ok {
					return ok, ver
				}
			}
		case []interface{}:
			for _, vv := range t {
				if ok, ver := walk(vv); ok {
					return ok, ver
				}
			}
		}
		return false, ""
	}
	ok, ver := walk(payload)
	return ok, ver, nil
}

func anyToString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		if v == nil {
			return ""
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func (s *RegistryServer) handleWebUIExecApprovals(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if strings.TrimSpace(s.configPath) == "" {
		http.Error(w, "config path not set", http.StatusInternalServerError)
		return
	}
	b, err := os.ReadFile(s.configPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(b, &cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodGet {
		toolsMap, _ := cfg["tools"].(map[string]interface{})
		shellMap, _ := toolsMap["shell"].(map[string]interface{})
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "exec_approvals": shellMap})
		return
	}
	if r.Method == http.MethodPost {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		toolsMap, _ := cfg["tools"].(map[string]interface{})
		if toolsMap == nil {
			toolsMap = map[string]interface{}{}
			cfg["tools"] = toolsMap
		}
		shellMap, _ := toolsMap["shell"].(map[string]interface{})
		if shellMap == nil {
			shellMap = map[string]interface{}{}
			toolsMap["shell"] = shellMap
		}
		for k, v := range body {
			shellMap[k] = v
		}
		out, _ := json.MarshalIndent(cfg, "", "  ")
		tmp := s.configPath + ".tmp"
		if err := os.WriteFile(tmp, out, 0644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.Rename(tmp, s.configPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if s.onConfigAfter != nil {
			s.onConfigAfter()
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "reloaded": true})
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *RegistryServer) handleWebUILogsStream(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimSpace(s.logFilePath)
	if path == "" {
		http.Error(w, "log path not configured", http.StatusInternalServerError)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	if fi != nil {
		_, _ = f.Seek(fi.Size(), io.SeekStart)
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	reader := bufio.NewReader(f)
	for {
		select {
		case <-r.Context().Done():
			return
		default:
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				trim := strings.TrimRight(line, "\r\n")
				if trim != "" {
					if json.Valid([]byte(trim)) {
						_, _ = w.Write([]byte(trim + "\n"))
					} else {
						fallback, _ := json.Marshal(map[string]interface{}{"time": time.Now().UTC().Format(time.RFC3339), "level": "INFO", "msg": trim})
						_, _ = w.Write(append(fallback, '\n'))
					}
					flusher.Flush()
				}
			}
			if err != nil {
				time.Sleep(500 * time.Millisecond)
			}
		}
	}
}

func (s *RegistryServer) checkAuth(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "Bearer "+s.token {
		return true
	}
	if strings.TrimSpace(r.URL.Query().Get("token")) == s.token {
		return true
	}
	if c, err := r.Cookie("clawgo_webui_token"); err == nil && strings.TrimSpace(c.Value) == s.token {
		return true
	}
	// Browser asset fallback: allow token propagated via Referer query.
	if ref := strings.TrimSpace(r.Referer()); ref != "" {
		if u, err := url.Parse(ref); err == nil {
			if strings.TrimSpace(u.Query().Get("token")) == s.token {
				return true
			}
		}
	}
	return false
}

func hotReloadFieldPaths() []string {
	return []string{
		"logging.*",
		"sentinel.*",
		"agents.*",
		"providers.*",
		"tools.*",
		"channels.*",
		"cron.*",
		"agents.defaults.heartbeat.*",
		"agents.defaults.autonomy.*",
		"gateway.* (except listen address/port may require restart in some environments)",
	}
}

const webUIHTML = `<!doctype html>
<html><head><meta charset="utf-8"/><meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>ClawGo WebUI</title>
<style>body{font-family:system-ui;margin:20px;max-width:980px}textarea{width:100%;min-height:220px}#chatlog{white-space:pre-wrap;border:1px solid #ddd;padding:12px;min-height:180px}</style>
</head><body>
<h2>ClawGo WebUI</h2>
<p>Token: <input id="token" placeholder="gateway token" style="width:320px"/></p>
<h3>Config (dynamic + hot reload)</h3>
<button onclick="loadCfg()">Load Config</button>
<button onclick="saveCfg()">Save + Reload</button>
<textarea id="cfg"></textarea>
<h3>Chat (supports media upload)</h3>
<div>Session: <input id="session" value="webui:default"/> <input id="msg" placeholder="message" style="width:420px"/> <input id="file" type="file"/> <button onclick="sendChat()">Send</button></div>
<div id="chatlog"></div>
<script>
function auth(){const t=document.getElementById('token').value.trim();return t?('?token='+encodeURIComponent(t)):''}
async function loadCfg(){let r=await fetch('/webui/api/config'+auth());document.getElementById('cfg').value=await r.text()}
async function saveCfg(){let j=JSON.parse(document.getElementById('cfg').value);let r=await fetch('/webui/api/config'+auth(),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(j)});alert(await r.text())}
async function sendChat(){
 let media='';const f=document.getElementById('file').files[0];
 if(f){let fd=new FormData();fd.append('file',f);let ur=await fetch('/webui/api/upload'+auth(),{method:'POST',body:fd});let uj=await ur.json();media=uj.path||''}
 const payload={session:document.getElementById('session').value,message:document.getElementById('msg').value,media};
 let r=await fetch('/webui/api/chat'+auth(),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});let t=await r.text();
 document.getElementById('chatlog').textContent += '\nUSER> '+payload.message+(media?(' [file:'+media+']'):'')+'\nBOT> '+t+'\n';
}
loadCfg();
</script></body></html>`
