package nodes

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	cfgpkg "clawgo/pkg/config"
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
	onChatHistory func(sessionKey string) []map[string]interface{}
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

func (s *RegistryServer) SetConfigPath(path string)    { s.configPath = strings.TrimSpace(path) }
func (s *RegistryServer) SetWorkspacePath(path string) { s.workspacePath = strings.TrimSpace(path) }
func (s *RegistryServer) SetLogFilePath(path string)   { s.logFilePath = strings.TrimSpace(path) }
func (s *RegistryServer) SetChatHandler(fn func(ctx context.Context, sessionKey, content string) (string, error)) {
	s.onChat = fn
}
func (s *RegistryServer) SetChatHistoryHandler(fn func(sessionKey string) []map[string]interface{}) {
	s.onChatHistory = fn
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
	mux.HandleFunc("/webui/api/chat/history", s.handleWebUIChatHistory)
	mux.HandleFunc("/webui/api/chat/stream", s.handleWebUIChatStream)
	mux.HandleFunc("/webui/api/version", s.handleWebUIVersion)
	mux.HandleFunc("/webui/api/upload", s.handleWebUIUpload)
	mux.HandleFunc("/webui/api/nodes", s.handleWebUINodes)
	mux.HandleFunc("/webui/api/cron", s.handleWebUICron)
	mux.HandleFunc("/webui/api/skills", s.handleWebUISkills)
	mux.HandleFunc("/webui/api/sessions", s.handleWebUISessions)
	mux.HandleFunc("/webui/api/memory", s.handleWebUIMemory)
	mux.HandleFunc("/webui/api/task_audit", s.handleWebUITaskAudit)
	mux.HandleFunc("/webui/api/exec_approvals", s.handleWebUIExecApprovals)
	mux.HandleFunc("/webui/api/logs/stream", s.handleWebUILogsStream)
	mux.HandleFunc("/webui/api/logs/recent", s.handleWebUILogsRecent)
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
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
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
			info := hotReloadFieldInfo()
			paths := make([]string, 0, len(info))
			for _, it := range info {
				if p, ok := it["path"].(string); ok && strings.TrimSpace(p) != "" {
					paths = append(paths, p)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":                       true,
				"config":                   cfg,
				"hot_reload_fields":        paths,
				"hot_reload_field_details": info,
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
		confirmRisky, _ := body["confirm_risky"].(bool)
		delete(body, "confirm_risky")

		oldCfgRaw, _ := os.ReadFile(s.configPath)
		var oldMap map[string]interface{}
		_ = json.Unmarshal(oldCfgRaw, &oldMap)

		riskyPaths := []string{
			"channels.telegram.token",
			"channels.telegram.allow_from",
			"channels.telegram.allow_chats",
			"providers.proxy.base_url",
			"providers.proxy.api_key",
			"gateway.token",
			"gateway.port",
		}
		changedRisky := make([]string, 0)
		for _, p := range riskyPaths {
			if fmt.Sprintf("%v", getPathValue(oldMap, p)) != fmt.Sprintf("%v", getPathValue(body, p)) {
				changedRisky = append(changedRisky, p)
			}
		}
		if len(changedRisky) > 0 && !confirmRisky {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":               false,
				"error":            "risky fields changed; confirmation required",
				"requires_confirm": true,
				"changed_fields":   changedRisky,
			})
			return
		}

		candidate, err := json.Marshal(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cfg := cfgpkg.DefaultConfig()
		dec := json.NewDecoder(bytes.NewReader(candidate))
		dec.DisallowUnknownFields()
		if err := dec.Decode(cfg); err != nil {
			http.Error(w, "config schema validation failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		if errs := cfgpkg.Validate(cfg); len(errs) > 0 {
			list := make([]string, 0, len(errs))
			for _, e := range errs {
				list = append(list, e.Error())
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "config validation failed", "details": list})
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

func getPathValue(m map[string]interface{}, path string) interface{} {
	if m == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	parts := strings.Split(path, ".")
	var cur interface{} = m
	for _, p := range parts {
		node, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = node[p]
	}
	return cur
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
	session := body.Session
	if session == "" {
		session = r.URL.Query().Get("session")
	}
	if session == "" {
		session = "main"
	}
	prompt := body.Message
	if body.Media != "" {
		if prompt != "" {
			prompt += "\n"
		}
		prompt += "[file: " + body.Media + "]"
	}
	resp, err := s.onChat(r.Context(), session, prompt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "reply": resp, "session": session})
}

func (s *RegistryServer) handleWebUIChatHistory(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session := r.URL.Query().Get("session")
	if session == "" {
		session = "main"
	}
	if s.onChatHistory == nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "session": session, "messages": []interface{}{}})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "session": session, "messages": s.onChatHistory(session)})
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
	session := body.Session
	if session == "" {
		session = r.URL.Query().Get("session")
	}
	if session == "" {
		session = "main"
	}
	prompt := body.Message
	if body.Media != "" {
		if prompt != "" {
			prompt += "\n"
		}
		prompt += "[file: " + body.Media + "]"
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

func (s *RegistryServer) handleWebUIVersion(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":              true,
		"gateway_version": gatewayBuildVersion(),
		"webui_version":   detectWebUIVersion(strings.TrimSpace(s.webUIDir)),
	})
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
		host, _ := os.Hostname()
		local := NodeInfo{ID: "local", Name: "local", Endpoint: "gateway", Version: gatewayBuildVersion(), LastSeenAt: time.Now(), Online: true}
		if strings.TrimSpace(host) != "" {
			local.Name = host
		}
		if ip := detectLocalIP(); ip != "" {
			local.Endpoint = ip
		}
		hostLower := strings.ToLower(strings.TrimSpace(host))
		matched := false
		for i := range list {
			id := strings.ToLower(strings.TrimSpace(list[i].ID))
			name := strings.ToLower(strings.TrimSpace(list[i].Name))
			if id == "local" || name == "local" || (hostLower != "" && name == hostLower) {
				// Always keep local node green/alive with latest ip+version
				list[i].ID = "local"
				list[i].Online = true
				list[i].Version = local.Version
				if strings.TrimSpace(local.Endpoint) != "" {
					list[i].Endpoint = local.Endpoint
				}
				if strings.TrimSpace(local.Name) != "" {
					list[i].Name = local.Name
				}
				list[i].LastSeenAt = time.Now()
				matched = true
				break
			}
		}
		if !matched {
			list = append([]NodeInfo{local}, list...)
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
		action := strings.ToLower(body.Action)
		if action != "delete" {
			http.Error(w, "unsupported action", http.StatusBadRequest)
			return
		}
		if s.mgr == nil {
			http.Error(w, "nodes manager unavailable", http.StatusInternalServerError)
			return
		}
		id := body.ID
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
	skillsDir := filepath.Join(s.workspacePath, "skills")
	if strings.TrimSpace(skillsDir) == "" {
		http.Error(w, "workspace not configured", http.StatusInternalServerError)
		return
	}
	_ = os.MkdirAll(skillsDir, 0755)

	resolveSkillPath := func(name string) (string, error) {
		name = strings.TrimSpace(name)
		if name == "" {
			return "", fmt.Errorf("name required")
		}
		cands := []string{
			filepath.Join(skillsDir, name),
			filepath.Join(skillsDir, name+".disabled"),
			filepath.Join("/root/clawgo/workspace/skills", name),
			filepath.Join("/root/clawgo/workspace/skills", name+".disabled"),
		}
		for _, p := range cands {
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				return p, nil
			}
		}
		return "", fmt.Errorf("skill not found: %s", name)
	}

	switch r.Method {
	case http.MethodGet:
		if id := strings.TrimSpace(r.URL.Query().Get("id")); id != "" {
			skillPath, err := resolveSkillPath(id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			if strings.TrimSpace(r.URL.Query().Get("files")) == "1" {
				var files []string
				_ = filepath.WalkDir(skillPath, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return nil
					}
					if d.IsDir() {
						return nil
					}
					rel, _ := filepath.Rel(skillPath, path)
					if strings.HasPrefix(rel, "..") {
						return nil
					}
					files = append(files, filepath.ToSlash(rel))
					return nil
				})
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": id, "files": files})
				return
			}
			if f := strings.TrimSpace(r.URL.Query().Get("file")); f != "" {
				clean := filepath.Clean(f)
				if strings.HasPrefix(clean, "..") {
					http.Error(w, "invalid file path", http.StatusBadRequest)
					return
				}
				full := filepath.Join(skillPath, clean)
				b, err := os.ReadFile(full)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": id, "file": filepath.ToSlash(clean), "content": string(b)})
				return
			}
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
			Source        string   `json:"source,omitempty"`
		}
		candDirs := []string{skillsDir, filepath.Join("/root/clawgo/workspace", "skills")}
		seenDirs := map[string]struct{}{}
		seenSkills := map[string]struct{}{}
		items := make([]skillItem, 0)
		checkUpdates := strings.TrimSpace(r.URL.Query().Get("check_updates")) != "0"

		for _, dir := range candDirs {
			dir = strings.TrimSpace(dir)
			if dir == "" {
				continue
			}
			if _, ok := seenDirs[dir]; ok {
				continue
			}
			seenDirs[dir] = struct{}{}
			entries, err := os.ReadDir(dir)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				name := e.Name()
				enabled := !strings.HasSuffix(name, ".disabled")
				baseName := strings.TrimSuffix(name, ".disabled")
				if _, ok := seenSkills[baseName]; ok {
					continue
				}
				seenSkills[baseName] = struct{}{}
				desc, tools, sys := readSkillMeta(filepath.Join(dir, name, "SKILL.md"))
				if desc == "" || len(tools) == 0 || sys == "" {
					d2, t2, s2 := readSkillMeta(filepath.Join(dir, baseName, "SKILL.md"))
					if desc == "" {
						desc = d2
					}
					if len(tools) == 0 {
						tools = t2
					}
					if sys == "" {
						sys = s2
					}
				}
				if tools == nil {
					tools = []string{}
				}
				it := skillItem{ID: baseName, Name: baseName, Description: desc, Tools: tools, SystemPrompt: sys, Enabled: enabled, UpdateChecked: checkUpdates, Source: dir}
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
		case "install":
			cmd := exec.CommandContext(r.Context(), "clawhub", "install", name)
			cmd.Dir = strings.TrimSpace(s.workspacePath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				http.Error(w, fmt.Sprintf("install failed: %v\n%s", err, string(out)), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "installed": name, "output": string(out)})
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
		case "write_file":
			skillPath, err := resolveSkillPath(name)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			filePath, _ := body["file"].(string)
			clean := filepath.Clean(strings.TrimSpace(filePath))
			if clean == "" || strings.HasPrefix(clean, "..") {
				http.Error(w, "invalid file path", http.StatusBadRequest)
				return
			}
			content, _ := body["content"].(string)
			full := filepath.Join(skillPath, clean)
			if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := os.WriteFile(full, []byte(content), 0644); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "name": name, "file": filepath.ToSlash(clean)})
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
	if desc == "" {
		desc = "No description provided."
	}
	if len(tools) == 0 {
		tools = []string{""}
	}
	toolLines := make([]string, 0, len(tools))
	for _, t := range tools {
		t = t
		if t == "" {
			continue
		}
		toolLines = append(toolLines, "- "+t)
	}
	if len(toolLines) == 0 {
		toolLines = []string{"- (none)"}
	}
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
`, name, desc, name, desc, strings.Join(toolLines, "\n"), systemPrompt)
}

func readSkillMeta(path string) (desc string, tools []string, systemPrompt string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", []string{}, ""
	}
	s := string(b)
	reDesc := regexp.MustCompile(`(?m)^description:\s*(.+)$`)
	reTools := regexp.MustCompile(`(?m)^##\s*Tools\s*$`)
	rePrompt := regexp.MustCompile(`(?m)^##\s*System Prompt\s*$`)
	if m := reDesc.FindStringSubmatch(s); len(m) > 1 {
		desc = m[1]
	}
	if loc := reTools.FindStringIndex(s); loc != nil {
		block := s[loc[1]:]
		if p := rePrompt.FindStringIndex(block); p != nil {
			block = block[:p[0]]
		}
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimPrefix(line, "-")
			if line != "" {
				tools = append(tools, line)
			}
		}
	}
	if tools == nil {
		tools = []string{}
	}
	if loc := rePrompt.FindStringIndex(s); loc != nil {
		systemPrompt = s[loc[1]:]
	}
	return
}

func gatewayBuildVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi != nil {
		ver := strings.TrimSpace(bi.Main.Version)
		rev := ""
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				rev = s.Value
				break
			}
		}
		if len(rev) > 8 {
			rev = rev[:8]
		}
		if ver == "" || ver == "(devel)" {
			ver = "devel"
		}
		if rev != "" {
			return ver + "+" + rev
		}
		return ver
	}
	return "unknown"
}

func detectWebUIVersion(webUIDir string) string {
	if strings.TrimSpace(webUIDir) == "" {
		return "unknown"
	}
	assets := filepath.Join(webUIDir, "assets")
	entries, err := os.ReadDir(assets)
	if err != nil {
		return "unknown"
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "index-") && strings.HasSuffix(name, ".js") {
			mid := strings.TrimSuffix(strings.TrimPrefix(name, "index-"), ".js")
			if mid != "" {
				return mid
			}
		}
	}
	return "unknown"
}

func detectLocalIP() string {
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				var ip net.IP
				switch v := a.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip == nil || ip.IsLoopback() {
					continue
				}
				ip = ip.To4()
				if ip == nil {
					continue
				}
				return ip.String()
			}
		}
	}
	// Fallback: detect outbound source IP.
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		if ua, ok := conn.LocalAddr().(*net.UDPAddr); ok && ua.IP != nil {
			if ip := ua.IP.To4(); ip != nil {
				return ip.String()
			}
		}
	}
	return ""
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
		kind, _ := sch["kind"].(string)
		if expr, ok := sch["expr"].(string); ok && expr != "" {
			out["expr"] = expr
		} else if strings.EqualFold(strings.TrimSpace(kind), "every") {
			if every, ok := sch["everyMs"].(float64); ok && every > 0 {
				out["expr"] = fmt.Sprintf("@every %s", (time.Duration(int64(every)) * time.Millisecond).String())
			}
		} else if strings.EqualFold(strings.TrimSpace(kind), "at") {
			if at, ok := sch["atMs"].(float64); ok && at > 0 {
				out["expr"] = time.UnixMilli(int64(at)).Format(time.RFC3339)
			}
		}
	}
	if payload, ok := m["payload"].(map[string]interface{}); ok {
		if msg, ok := payload["message"]; ok {
			out["message"] = msg
		}
		if d, ok := payload["deliver"]; ok {
			out["deliver"] = d
		}
		if c, ok := payload["channel"]; ok {
			out["channel"] = c
		}
		if to, ok := payload["to"]; ok {
			out["to"] = to
		}
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
	skill = skill
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
				ver := anyToString(t["version"])
				if ver == "" {
					ver = anyToString(t["latest_version"])
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

func (s *RegistryServer) handleWebUISessions(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionsDir := filepath.Join(filepath.Dir(s.workspacePath), "agents", "main", "sessions")
	_ = os.MkdirAll(sessionsDir, 0755)
	type item struct {
		Key     string `json:"key"`
		Channel string `json:"channel,omitempty"`
	}
	out := make([]item, 0, 16)
	entries, err := os.ReadDir(sessionsDir)
	if err == nil {
		seen := map[string]struct{}{}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".jsonl") || strings.Contains(name, ".deleted.") {
				continue
			}
			key := strings.TrimSuffix(name, ".jsonl")
			if strings.TrimSpace(key) == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			channel := ""
			if i := strings.Index(key, ":"); i > 0 {
				channel = key[:i]
			}
			out = append(out, item{Key: key, Channel: channel})
		}
	}
	if len(out) == 0 {
		out = append(out, item{Key: "main", Channel: "main"})
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "sessions": out})
}

func (s *RegistryServer) handleWebUIMemory(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	memoryDir := filepath.Join(s.workspacePath, "memory")
	_ = os.MkdirAll(memoryDir, 0755)
	switch r.Method {
	case http.MethodGet:
		path := strings.TrimSpace(r.URL.Query().Get("path"))
		if path == "" {
			entries, err := os.ReadDir(memoryDir)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			files := make([]string, 0, len(entries))
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				files = append(files, e.Name())
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "files": files})
			return
		}
		clean := filepath.Clean(path)
		if strings.HasPrefix(clean, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		full := filepath.Join(memoryDir, clean)
		b, err := os.ReadFile(full)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "path": clean, "content": string(b)})
	case http.MethodPost:
		var body struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		clean := filepath.Clean(body.Path)
		if clean == "" || strings.HasPrefix(clean, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		full := filepath.Join(memoryDir, clean)
		if err := os.WriteFile(full, []byte(body.Content), 0644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "path": clean})
	case http.MethodDelete:
		path := filepath.Clean(r.URL.Query().Get("path"))
		if path == "" || strings.HasPrefix(path, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		full := filepath.Join(memoryDir, path)
		if err := os.Remove(full); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "deleted": true, "path": path})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *RegistryServer) handleWebUITaskAudit(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := filepath.Join(strings.TrimSpace(s.workspacePath), "memory", "task-audit.jsonl")
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 500 {
				n = 500
			}
			limit = n
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "items": []map[string]interface{}{}})
		return
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	items := make([]map[string]interface{}, 0, len(lines))
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		var row map[string]interface{}
		if err := json.Unmarshal([]byte(ln), &row); err == nil {
			items = append(items, row)
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "items": items})
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

func (s *RegistryServer) handleWebUILogsRecent(w http.ResponseWriter, r *http.Request) {
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
	limit := 10
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	start := 0
	if len(lines) > limit {
		start = len(lines) - limit
	}
	out := make([]map[string]interface{}, 0, limit)
	for _, ln := range lines[start:] {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if json.Valid([]byte(ln)) {
			var m map[string]interface{}
			if err := json.Unmarshal([]byte(ln), &m); err == nil {
				out = append(out, m)
				continue
			}
		}
		out = append(out, map[string]interface{}{"time": time.Now().UTC().Format(time.RFC3339), "level": "INFO", "msg": ln})
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "logs": out})
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

func hotReloadFieldInfo() []map[string]interface{} {
	return []map[string]interface{}{
		{"path": "logging.*", "name": "Logging", "description": "Log level, persistence, and related settings"},
		{"path": "sentinel.*", "name": "Sentinel", "description": "Health checks and auto-heal behavior"},
		{"path": "agents.*", "name": "Agent", "description": "Models, policies, and default behavior"},
		{"path": "providers.*", "name": "Providers", "description": "LLM providers and proxy settings"},
		{"path": "tools.*", "name": "Tools", "description": "Tool toggles and runtime options"},
		{"path": "channels.*", "name": "Channels", "description": "Telegram and other channel settings"},
		{"path": "cron.*", "name": "Cron", "description": "Global cron runtime settings"},
		{"path": "agents.defaults.heartbeat.*", "name": "Heartbeat", "description": "Heartbeat interval and prompt template"},
		{"path": "agents.defaults.autonomy.*", "name": "Autonomy", "description": "Autonomy toggles and throttling"},
		{"path": "gateway.*", "name": "Gateway", "description": "Mostly hot-reloadable; host/port may require restart"},
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
