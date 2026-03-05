package nodes

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	ekgCacheMu    sync.Mutex
	ekgCachePath  string
	ekgCacheStamp time.Time
	ekgCacheSize  int64
	ekgCacheRows  []map[string]interface{}
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
	mux.HandleFunc("/webui/api/task_queue", s.handleWebUITaskQueue)
	mux.HandleFunc("/webui/api/tasks", s.handleWebUITasks)
	mux.HandleFunc("/webui/api/task_daily_summary", s.handleWebUITaskDailySummary)
	mux.HandleFunc("/webui/api/ekg_stats", s.handleWebUIEKGStats)
	mux.HandleFunc("/webui/api/office_state", s.handleWebUIOfficeState)
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
		cfgDefault := cfgpkg.DefaultConfig()
		defBytes, _ := json.Marshal(cfgDefault)
		var merged map[string]interface{}
		_ = json.Unmarshal(defBytes, &merged)
		var loaded map[string]interface{}
		if err := json.Unmarshal(b, &loaded); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		merged = mergeJSONMap(merged, loaded)

		if r.URL.Query().Get("include_hot_reload_fields") == "1" || strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("mode")), "hot") {
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
				"config":                   merged,
				"hot_reload_fields":        paths,
				"hot_reload_field_details": info,
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		out, _ := json.MarshalIndent(merged, "", "  ")
		_, _ = w.Write(out)
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
			_ = requestSelfReloadSignal()
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "reloaded": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func mergeJSONMap(base, override map[string]interface{}) map[string]interface{} {
	if base == nil {
		base = map[string]interface{}{}
	}
	for k, v := range override {
		if bv, ok := base[k]; ok {
			bm, ok1 := bv.(map[string]interface{})
			om, ok2 := v.(map[string]interface{})
			if ok1 && ok2 {
				base[k] = mergeJSONMap(bm, om)
				continue
			}
		}
		base[k] = v
	}
	return base
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
		clawhubPath := strings.TrimSpace(resolveClawHubBinary(r.Context()))
		clawhubInstalled := clawhubPath != ""
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
		// Default off to avoid hammering clawhub search API on each UI refresh.
		// Enable explicitly with ?check_updates=1 when needed.
		checkUpdates := strings.TrimSpace(r.URL.Query().Get("check_updates")) == "1"

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
				it := skillItem{ID: baseName, Name: baseName, Description: desc, Tools: tools, SystemPrompt: sys, Enabled: enabled, UpdateChecked: checkUpdates && clawhubInstalled, Source: dir}
				if checkUpdates && clawhubInstalled {
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":                true,
			"skills":            items,
			"source":            "clawhub",
			"clawhub_installed": clawhubInstalled,
			"clawhub_path":      clawhubPath,
		})

	case http.MethodPost:
		ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
		if strings.Contains(ct, "multipart/form-data") {
			imported, err := importSkillArchiveFromMultipart(r, skillsDir)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "imported": imported})
			return
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action, _ := body["action"].(string)
		action = strings.ToLower(strings.TrimSpace(action))
		if action == "install_clawhub" {
			output, err := ensureClawHubReady(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":           true,
				"output":       output,
				"installed":    true,
				"clawhub_path": resolveClawHubBinary(r.Context()),
			})
			return
		}
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
			clawhubPath := strings.TrimSpace(resolveClawHubBinary(r.Context()))
			if clawhubPath == "" {
				http.Error(w, "clawhub is not installed. please install clawhub first.", http.StatusPreconditionFailed)
				return
			}
			ignoreSuspicious := false
			switch v := body["ignore_suspicious"].(type) {
			case bool:
				ignoreSuspicious = v
			case string:
				ignoreSuspicious = strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1"
			}
			args := []string{"install", name}
			if ignoreSuspicious {
				args = append(args, "--force")
			}
			cmd := exec.CommandContext(r.Context(), clawhubPath, args...)
			cmd.Dir = strings.TrimSpace(s.workspacePath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				outText := string(out)
				lower := strings.ToLower(outText)
				if strings.Contains(lower, "rate limit exceeded") || strings.Contains(lower, "too many requests") {
					http.Error(w, fmt.Sprintf("clawhub rate limit exceeded. please retry later or configure auth token.\n%s", outText), http.StatusTooManyRequests)
					return
				}
				http.Error(w, fmt.Sprintf("install failed: %v\n%s", err, outText), http.StatusInternalServerError)
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
	if skill == "" {
		return false, "", fmt.Errorf("skill empty")
	}
	clawhubPath := strings.TrimSpace(resolveClawHubBinary(ctx))
	if clawhubPath == "" {
		return false, "", fmt.Errorf("clawhub not installed")
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, clawhubPath, "search", skill, "--json")
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

func resolveClawHubBinary(ctx context.Context) string {
	if p, err := exec.LookPath("clawhub"); err == nil {
		return p
	}
	prefix := strings.TrimSpace(npmGlobalPrefix(ctx))
	if prefix != "" {
		cand := filepath.Join(prefix, "bin", "clawhub")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	cands := []string{
		"/usr/local/bin/clawhub",
		"/opt/homebrew/bin/clawhub",
		filepath.Join(os.Getenv("HOME"), ".npm-global", "bin", "clawhub"),
	}
	for _, cand := range cands {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return ""
}

func npmGlobalPrefix(ctx context.Context) string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "npm", "config", "get", "prefix").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func runInstallCommand(ctx context.Context, cmdline string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, "sh", "-c", cmdline)
	out, err := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		if msg == "" {
			msg = err.Error()
		}
		return msg, fmt.Errorf("%s", msg)
	}
	return msg, nil
}

func ensureNodeRuntime(ctx context.Context) (string, error) {
	if nodePath, err := exec.LookPath("node"); err == nil {
		if _, err := exec.LookPath("npm"); err == nil {
			if major, verr := detectNodeMajor(ctx, nodePath); verr == nil && major == 22 {
				return "node@22 and npm already installed", nil
			}
		}
	}

	var output []string
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("brew"); err != nil {
			return strings.Join(output, "\n"), fmt.Errorf("nodejs/npm missing and Homebrew not found; please install Homebrew then retry")
		}
		out, err := runInstallCommand(ctx, "brew install node@22 && brew link --overwrite --force node@22")
		if out != "" {
			output = append(output, out)
		}
		if err != nil {
			return strings.Join(output, "\n"), err
		}
	case "linux":
		var out string
		var err error
		switch {
		case commandExists("apt-get"):
			if commandExists("curl") {
				out, err = runInstallCommand(ctx, "curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && apt-get install -y nodejs")
			} else if commandExists("wget") {
				out, err = runInstallCommand(ctx, "wget -qO- https://deb.nodesource.com/setup_22.x | bash - && apt-get install -y nodejs")
			} else {
				err = fmt.Errorf("missing curl/wget required for NodeSource setup_22.x")
			}
		case commandExists("dnf"):
			if commandExists("curl") {
				out, err = runInstallCommand(ctx, "curl -fsSL https://rpm.nodesource.com/setup_22.x | bash - && dnf install -y nodejs")
			} else if commandExists("wget") {
				out, err = runInstallCommand(ctx, "wget -qO- https://rpm.nodesource.com/setup_22.x | bash - && dnf install -y nodejs")
			} else {
				err = fmt.Errorf("missing curl/wget required for NodeSource setup_22.x")
			}
		case commandExists("yum"):
			if commandExists("curl") {
				out, err = runInstallCommand(ctx, "curl -fsSL https://rpm.nodesource.com/setup_22.x | bash - && yum install -y nodejs")
			} else if commandExists("wget") {
				out, err = runInstallCommand(ctx, "wget -qO- https://rpm.nodesource.com/setup_22.x | bash - && yum install -y nodejs")
			} else {
				err = fmt.Errorf("missing curl/wget required for NodeSource setup_22.x")
			}
		case commandExists("pacman"):
			out, err = runInstallCommand(ctx, "pacman -Sy --noconfirm nodejs npm")
		case commandExists("apk"):
			out, err = runInstallCommand(ctx, "apk add --no-cache nodejs npm")
		default:
			return strings.Join(output, "\n"), fmt.Errorf("nodejs/npm missing and no supported package manager found")
		}
		if out != "" {
			output = append(output, out)
		}
		if err != nil {
			return strings.Join(output, "\n"), err
		}
	default:
		return strings.Join(output, "\n"), fmt.Errorf("unsupported OS for auto install: %s", runtime.GOOS)
	}

	if _, err := exec.LookPath("node"); err != nil {
		return strings.Join(output, "\n"), fmt.Errorf("node installation completed but `node` still not found in PATH")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		return strings.Join(output, "\n"), fmt.Errorf("node installation completed but `npm` still not found in PATH")
	}
	nodePath, _ := exec.LookPath("node")
	major, err := detectNodeMajor(ctx, nodePath)
	if err != nil {
		return strings.Join(output, "\n"), fmt.Errorf("failed to detect node major version: %w", err)
	}
	if major != 22 {
		return strings.Join(output, "\n"), fmt.Errorf("node version is %d, expected 22", major)
	}
	output = append(output, "node@22/npm installed")
	return strings.Join(output, "\n"), nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func detectNodeMajor(ctx context.Context, nodePath string) (int, error) {
	nodePath = strings.TrimSpace(nodePath)
	if nodePath == "" {
		return 0, fmt.Errorf("node path empty")
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, nodePath, "-p", "process.versions.node.split('.')[0]").Output()
	if err != nil {
		return 0, err
	}
	majorStr := strings.TrimSpace(string(out))
	if majorStr == "" {
		return 0, fmt.Errorf("empty node major version")
	}
	v, err := strconv.Atoi(majorStr)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func ensureClawHubReady(ctx context.Context) (string, error) {
	outs := make([]string, 0, 4)
	if p := resolveClawHubBinary(ctx); p != "" {
		return "clawhub already installed at: " + p, nil
	}
	nodeOut, err := ensureNodeRuntime(ctx)
	if nodeOut != "" {
		outs = append(outs, nodeOut)
	}
	if err != nil {
		return strings.Join(outs, "\n"), err
	}
	clawOut, err := runInstallCommand(ctx, "npm i -g clawhub")
	if clawOut != "" {
		outs = append(outs, clawOut)
	}
	if err != nil {
		return strings.Join(outs, "\n"), err
	}
	if p := resolveClawHubBinary(ctx); p != "" {
		outs = append(outs, "clawhub installed at: "+p)
		return strings.Join(outs, "\n"), nil
	}
	return strings.Join(outs, "\n"), fmt.Errorf("installed clawhub but executable still not found in PATH")
}

func importSkillArchiveFromMultipart(r *http.Request, skillsDir string) ([]string, error) {
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		return nil, err
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("file required")
	}
	defer f.Close()

	uploadDir := filepath.Join(os.TempDir(), "clawgo_skill_uploads")
	_ = os.MkdirAll(uploadDir, 0755)
	archivePath := filepath.Join(uploadDir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(h.Filename)))
	out, err := os.Create(archivePath)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(out, f); err != nil {
		_ = out.Close()
		_ = os.Remove(archivePath)
		return nil, err
	}
	_ = out.Close()
	defer os.Remove(archivePath)

	extractDir, err := os.MkdirTemp("", "clawgo_skill_extract_*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(extractDir)

	if err := extractArchive(archivePath, extractDir); err != nil {
		return nil, err
	}

	type candidate struct {
		name string
		dir  string
	}
	candidates := make([]candidate, 0)
	seen := map[string]struct{}{}
	err = filepath.WalkDir(extractDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), "SKILL.md") {
			dir := filepath.Dir(path)
			rel, relErr := filepath.Rel(extractDir, dir)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(strings.TrimSpace(rel))
			if rel == "" {
				rel = "."
			}
			name := filepath.Base(rel)
			if rel == "." {
				name = archiveBaseName(h.Filename)
			}
			name = sanitizeSkillName(name)
			if name == "" {
				return nil
			}
			if _, ok := seen[name]; ok {
				return nil
			}
			seen[name] = struct{}{}
			candidates = append(candidates, candidate{name: name, dir: dir})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no SKILL.md found in archive")
	}

	imported := make([]string, 0, len(candidates))
	for _, c := range candidates {
		dst := filepath.Join(skillsDir, c.name)
		if _, err := os.Stat(dst); err == nil {
			return nil, fmt.Errorf("skill already exists: %s", c.name)
		}
		if _, err := os.Stat(dst + ".disabled"); err == nil {
			return nil, fmt.Errorf("disabled skill already exists: %s", c.name)
		}
		if err := copyDir(c.dir, dst); err != nil {
			return nil, err
		}
		imported = append(imported, c.name)
	}
	sort.Strings(imported)
	return imported, nil
}

func archiveBaseName(filename string) string {
	name := filepath.Base(strings.TrimSpace(filename))
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return name[:len(name)-len(".tar.gz")]
	case strings.HasSuffix(lower, ".tgz"):
		return name[:len(name)-len(".tgz")]
	case strings.HasSuffix(lower, ".zip"):
		return name[:len(name)-len(".zip")]
	case strings.HasSuffix(lower, ".tar"):
		return name[:len(name)-len(".tar")]
	default:
		ext := filepath.Ext(name)
		return strings.TrimSuffix(name, ext)
	}
}

func sanitizeSkillName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, ch := range strings.ToLower(name) {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			b.WriteRune(ch)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" || out == "." {
		return ""
	}
	return out
}

func extractArchive(archivePath, targetDir string) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, targetDir)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archivePath, targetDir)
	case strings.HasSuffix(lower, ".tar"):
		return extractTar(archivePath, targetDir)
	default:
		return fmt.Errorf("unsupported archive format: %s", filepath.Base(archivePath))
	}
}

func extractZip(archivePath, targetDir string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if err := writeArchivedEntry(targetDir, f.Name, f.FileInfo().IsDir(), func() (io.ReadCloser, error) {
			return f.Open()
		}); err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(archivePath, targetDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	return extractTarReader(tar.NewReader(gz), targetDir)
}

func extractTar(archivePath, targetDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	return extractTarReader(tar.NewReader(f), targetDir)
}

func extractTarReader(tr *tar.Reader, targetDir string) error {
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := writeArchivedEntry(targetDir, hdr.Name, true, nil); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			name := hdr.Name
			if err := writeArchivedEntry(targetDir, name, false, func() (io.ReadCloser, error) {
				return io.NopCloser(tr), nil
			}); err != nil {
				return err
			}
		}
	}
}

func writeArchivedEntry(targetDir, name string, isDir bool, opener func() (io.ReadCloser, error)) error {
	clean := filepath.Clean(strings.TrimSpace(name))
	clean = strings.TrimPrefix(clean, string(filepath.Separator))
	clean = strings.TrimPrefix(clean, "/")
	for strings.HasPrefix(clean, "../") {
		clean = strings.TrimPrefix(clean, "../")
	}
	if clean == "." || clean == "" {
		return nil
	}
	dst := filepath.Join(targetDir, clean)
	absTarget, _ := filepath.Abs(targetDir)
	absDst, _ := filepath.Abs(dst)
	if !strings.HasPrefix(absDst, absTarget+string(filepath.Separator)) && absDst != absTarget {
		return fmt.Errorf("invalid archive entry path: %s", name)
	}
	if isDir {
		return os.MkdirAll(dst, 0755)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	rc, err := opener()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		info, err := e.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		in, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		out, err := os.Create(dstPath)
		if err != nil {
			_ = in.Close()
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			_ = in.Close()
			return err
		}
		_ = out.Close()
		_ = in.Close()
	}
	return nil
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
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodPost {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action := fmt.Sprintf("%v", body["action"])
		taskID := fmt.Sprintf("%v", body["task_id"])
		if taskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}
		tasksPath := filepath.Join(strings.TrimSpace(s.workspacePath), "memory", "tasks.json")
		tb, err := os.ReadFile(tasksPath)
		if err != nil {
			http.Error(w, "tasks not found", http.StatusNotFound)
			return
		}
		var tasks []map[string]interface{}
		if err := json.Unmarshal(tb, &tasks); err != nil {
			http.Error(w, "invalid tasks file", http.StatusInternalServerError)
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		updated := false
		for _, t := range tasks {
			if fmt.Sprintf("%v", t["id"]) != taskID {
				continue
			}
			switch action {
			case "pause":
				t["status"] = "waiting"
				t["block_reason"] = "manual_pause"
				t["last_pause_reason"] = "manual_pause"
				t["last_pause_at"] = now
			case "retry":
				t["status"] = "todo"
				t["block_reason"] = ""
				t["retry_after"] = ""
			case "complete":
				t["status"] = "done"
				t["block_reason"] = "manual_complete"
			case "ignore":
				t["status"] = "blocked"
				t["block_reason"] = "manual_ignore"
				t["retry_after"] = "2099-01-01T00:00:00Z"
			default:
				http.Error(w, "unsupported action", http.StatusBadRequest)
				return
			}
			t["updated_at"] = now
			updated = true
			break
		}
		if !updated {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}
		out, _ := json.MarshalIndent(tasks, "", "  ")
		if err := os.WriteFile(tasksPath, out, 0644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		return
	}

	path := filepath.Join(strings.TrimSpace(s.workspacePath), "memory", "task-audit.jsonl")
	includeHeartbeat := r.URL.Query().Get("include_heartbeat") == "1"
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
			source := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["source"])))
			if !includeHeartbeat && source == "heartbeat" {
				continue
			}
			items = append(items, row)
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "items": items})
}

func (s *RegistryServer) handleWebUITaskQueue(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := filepath.Join(strings.TrimSpace(s.workspacePath), "memory", "task-audit.jsonl")
	includeHeartbeat := r.URL.Query().Get("include_heartbeat") == "1"
	b, err := os.ReadFile(path)
	lines := []string{}
	if err == nil {
		lines = strings.Split(string(b), "\n")
	}
	type agg struct {
		Last     map[string]interface{}
		Logs     []string
		Attempts int
	}
	m := map[string]*agg{}
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		var row map[string]interface{}
		if err := json.Unmarshal([]byte(ln), &row); err != nil {
			continue
		}
		source := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["source"])))
		if !includeHeartbeat && source == "heartbeat" {
			continue
		}
		id := fmt.Sprintf("%v", row["task_id"])
		if id == "" {
			continue
		}
		if _, ok := m[id]; !ok {
			m[id] = &agg{Last: row, Logs: []string{}, Attempts: 0}
		}
		a := m[id]
		a.Last = row
		a.Attempts++
		if lg := strings.TrimSpace(fmt.Sprintf("%v", row["log"])); lg != "" {
			if len(a.Logs) == 0 || a.Logs[len(a.Logs)-1] != lg {
				a.Logs = append(a.Logs, lg)
				if len(a.Logs) > 20 {
					a.Logs = a.Logs[len(a.Logs)-20:]
				}
			}
		}
	}
	items := make([]map[string]interface{}, 0, len(m))
	running := make([]map[string]interface{}, 0)
	for _, a := range m {
		row := a.Last
		row["logs"] = a.Logs
		row["attempts"] = a.Attempts
		items = append(items, row)
		if fmt.Sprintf("%v", row["status"]) == "running" {
			running = append(running, row)
		}
	}

	// Merge autonomy queue states (including waiting/blocked-by-user) for full audit visibility.
	tasksPath := filepath.Join(strings.TrimSpace(s.workspacePath), "memory", "tasks.json")
	if tb, err := os.ReadFile(tasksPath); err == nil {
		var tasks []map[string]interface{}
		if json.Unmarshal(tb, &tasks) == nil {
			seen := map[string]struct{}{}
			for _, it := range items {
				seen[fmt.Sprintf("%v", it["task_id"])] = struct{}{}
			}
			for _, t := range tasks {
				id := fmt.Sprintf("%v", t["id"])
				if id == "" {
					continue
				}
				if _, ok := seen[id]; ok {
					continue
				}
				row := map[string]interface{}{
					"task_id":           id,
					"time":              t["updated_at"],
					"status":            t["status"],
					"source":            t["source"],
					"idle_run":          true,
					"input_preview":     t["content"],
					"block_reason":      t["block_reason"],
					"last_pause_reason": t["last_pause_reason"],
					"last_pause_at":     t["last_pause_at"],
					"logs":              []string{fmt.Sprintf("autonomy state: %v", t["status"])},
					"retry_count":       0,
				}
				items = append(items, row)
				if fmt.Sprintf("%v", row["status"]) == "running" {
					running = append(running, row)
				}
			}
		}
	}

	// Merge command watchdog queue from memory/task_queue.json for visibility.
	queuePath := filepath.Join(strings.TrimSpace(s.workspacePath), "memory", "task_queue.json")
	if qb, qErr := os.ReadFile(queuePath); qErr == nil {
		var q map[string]interface{}
		if json.Unmarshal(qb, &q) == nil {
			if arr, ok := q["running"].([]interface{}); ok {
				for _, item := range arr {
					row, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					id := fmt.Sprintf("%v", row["id"])
					if strings.TrimSpace(id) == "" {
						continue
					}
					label := fmt.Sprintf("%v", row["label"])
					source := strings.TrimSpace(fmt.Sprintf("%v", row["source"]))
					if source == "" {
						source = "command_watchdog"
					}
					rec := map[string]interface{}{
						"task_id":       "cmd:" + id,
						"time":          fmt.Sprintf("%v", row["started_at"]),
						"status":        "running",
						"source":        "command_watchdog",
						"channel":       source,
						"session":       "watchdog:" + id,
						"input_preview": label,
						"duration_ms":   0,
						"attempts":      1,
						"retry_count":   0,
						"logs": []string{
							fmt.Sprintf("watchdog source=%s heavy=%v", source, row["heavy"]),
							fmt.Sprintf("next_check_at=%v stalled_rounds=%v/%v", row["next_check_at"], row["stalled_rounds"], row["stall_round_limit"]),
						},
						"idle_run": true,
					}
					items = append(items, rec)
					running = append(running, rec)
				}
			}
			if arr, ok := q["waiting"].([]interface{}); ok {
				for _, item := range arr {
					row, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					id := fmt.Sprintf("%v", row["id"])
					if strings.TrimSpace(id) == "" {
						continue
					}
					label := fmt.Sprintf("%v", row["label"])
					source := strings.TrimSpace(fmt.Sprintf("%v", row["source"]))
					if source == "" {
						source = "command_watchdog"
					}
					rec := map[string]interface{}{
						"task_id":       "cmd:" + id,
						"time":          fmt.Sprintf("%v", row["enqueued_at"]),
						"status":        "waiting",
						"source":        "command_watchdog",
						"channel":       source,
						"session":       "watchdog:" + id,
						"input_preview": label,
						"duration_ms":   0,
						"attempts":      1,
						"retry_count":   0,
						"logs": []string{
							fmt.Sprintf("watchdog source=%s heavy=%v", source, row["heavy"]),
							fmt.Sprintf("enqueued_at=%v", row["enqueued_at"]),
						},
						"idle_run": true,
					}
					items = append(items, rec)
				}
			}
			if wd, ok := q["watchdog"].(map[string]interface{}); ok {
				items = append(items, map[string]interface{}{
					"task_id":       "cmd:watchdog",
					"time":          fmt.Sprintf("%v", q["time"]),
					"status":        "running",
					"source":        "command_watchdog",
					"channel":       "watchdog",
					"session":       "watchdog:stats",
					"input_preview": "command watchdog capacity snapshot",
					"duration_ms":   0,
					"attempts":      1,
					"retry_count":   0,
					"logs": []string{
						fmt.Sprintf("cpu_total=%v usage_ratio=%v reserve_pct=%v", wd["cpu_total"], wd["usage_ratio"], wd["reserve_pct"]),
						fmt.Sprintf("active=%v/%v heavy=%v/%v waiting=%v running=%v", wd["active"], wd["max_active"], wd["active_heavy"], wd["max_heavy"], wd["waiting"], wd["running"]),
					},
					"idle_run": true,
				})
			}
		}
	}

	sort.Slice(items, func(i, j int) bool { return fmt.Sprintf("%v", items[i]["time"]) > fmt.Sprintf("%v", items[j]["time"]) })
	stats := map[string]int{"total": len(items), "running": len(running), "idle_round_budget": 0, "active_user": 0, "manual_pause": 0}
	for _, it := range items {
		reason := fmt.Sprintf("%v", it["block_reason"])
		switch reason {
		case "idle_round_budget":
			stats["idle_round_budget"]++
		case "active_user":
			stats["active_user"]++
		case "manual_pause":
			stats["manual_pause"]++
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "running": running, "items": items, "stats": stats})
}

func (s *RegistryServer) handleWebUITaskDailySummary(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	path := filepath.Join(strings.TrimSpace(s.workspacePath), "memory", date+".md")
	b, err := os.ReadFile(path)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "date": date, "report": ""})
		return
	}
	text := string(b)
	marker := "## Autonomy Daily Report (" + date + ")"
	idx := strings.Index(text, marker)
	report := ""
	if idx >= 0 {
		report = text[idx:]
		if n := strings.Index(report[len(marker):], "\n## "); n > 0 {
			report = report[:len(marker)+n]
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "date": date, "report": report})
}

func (s *RegistryServer) loadEKGRowsCached(path string, maxLines int) []map[string]interface{} {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	s.ekgCacheMu.Lock()
	defer s.ekgCacheMu.Unlock()
	if s.ekgCachePath == path && s.ekgCacheSize == fi.Size() && s.ekgCacheStamp.Equal(fi.ModTime()) && len(s.ekgCacheRows) > 0 {
		return s.ekgCacheRows
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	rows := make([]map[string]interface{}, 0, len(lines))
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		var row map[string]interface{}
		if json.Unmarshal([]byte(ln), &row) == nil {
			rows = append(rows, row)
		}
	}
	s.ekgCachePath = path
	s.ekgCacheSize = fi.Size()
	s.ekgCacheStamp = fi.ModTime()
	s.ekgCacheRows = rows
	return rows
}

func (s *RegistryServer) handleWebUIEKGStats(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspace := strings.TrimSpace(s.workspacePath)
	ekgPath := filepath.Join(workspace, "memory", "ekg-events.jsonl")
	window := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("window")))
	windowDur := 24 * time.Hour
	switch window {
	case "6h":
		windowDur = 6 * time.Hour
	case "24h", "":
		windowDur = 24 * time.Hour
	case "7d":
		windowDur = 7 * 24 * time.Hour
	}
	selectedWindow := window
	if selectedWindow == "" {
		selectedWindow = "24h"
	}
	cutoff := time.Now().UTC().Add(-windowDur)
	rows := s.loadEKGRowsCached(ekgPath, 3000)
	type kv struct {
		Key   string  `json:"key"`
		Score float64 `json:"score,omitempty"`
		Count int     `json:"count,omitempty"`
	}
	providerScore := map[string]float64{}
	providerScoreWorkload := map[string]float64{}
	errSigCount := map[string]int{}
	errSigHeartbeat := map[string]int{}
	errSigWorkload := map[string]int{}
	sourceStats := map[string]int{}
	channelStats := map[string]int{}
	for _, row := range rows {
		ts := strings.TrimSpace(fmt.Sprintf("%v", row["time"]))
		if ts != "" {
			if tm, err := time.Parse(time.RFC3339, ts); err == nil {
				if tm.Before(cutoff) {
					continue
				}
			}
		}
		provider := strings.TrimSpace(fmt.Sprintf("%v", row["provider"]))
		status := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["status"])))
		errSig := strings.TrimSpace(fmt.Sprintf("%v", row["errsig"]))
		source := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["source"])))
		channel := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["channel"])))
		if source == "" {
			source = "unknown"
		}
		if channel == "" {
			channel = "unknown"
		}
		sourceStats[source]++
		channelStats[channel]++
		isHeartbeat := source == "heartbeat"
		if provider != "" {
			switch status {
			case "success":
				providerScore[provider] += 1
				if !isHeartbeat {
					providerScoreWorkload[provider] += 1
				}
			case "suppressed":
				providerScore[provider] += 0.2
				if !isHeartbeat {
					providerScoreWorkload[provider] += 0.2
				}
			case "error":
				providerScore[provider] -= 1
				if !isHeartbeat {
					providerScoreWorkload[provider] -= 1
				}
			}
		}
		if errSig != "" && status == "error" {
			errSigCount[errSig]++
			if isHeartbeat {
				errSigHeartbeat[errSig]++
			} else {
				errSigWorkload[errSig]++
			}
		}
	}
	toTopScore := func(m map[string]float64, n int) []kv {
		out := make([]kv, 0, len(m))
		for k, v := range m {
			out = append(out, kv{Key: k, Score: v})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
		if len(out) > n {
			out = out[:n]
		}
		return out
	}
	toTopCount := func(m map[string]int, n int) []kv {
		out := make([]kv, 0, len(m))
		for k, v := range m {
			out = append(out, kv{Key: k, Count: v})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
		if len(out) > n {
			out = out[:n]
		}
		return out
	}
	escalations := 0
	tasksPath := filepath.Join(workspace, "memory", "tasks.json")
	if tb, err := os.ReadFile(tasksPath); err == nil {
		var tasks []map[string]interface{}
		if json.Unmarshal(tb, &tasks) == nil {
			for _, t := range tasks {
				if strings.TrimSpace(fmt.Sprintf("%v", t["block_reason"])) == "repeated_error_signature" {
					escalations++
				}
			}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":                    true,
		"window":                selectedWindow,
		"provider_top":          toTopScore(providerScore, 5),
		"provider_top_workload": toTopScore(providerScoreWorkload, 5),
		"errsig_top":            toTopCount(errSigCount, 5),
		"errsig_top_heartbeat":  toTopCount(errSigHeartbeat, 5),
		"errsig_top_workload":   toTopCount(errSigWorkload, 5),
		"source_stats":          sourceStats,
		"channel_stats":         channelStats,
		"escalation_count":      escalations,
	})
}

func (s *RegistryServer) handleWebUIOfficeState(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspace := strings.TrimSpace(s.workspacePath)
	auditPath := filepath.Join(workspace, "memory", "task-audit.jsonl")
	tasksPath := filepath.Join(workspace, "memory", "tasks.json")
	ekgPath := filepath.Join(workspace, "memory", "ekg-events.jsonl")
	now := time.Now().UTC()

	parseTime := func(raw string) time.Time {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return time.Time{}
		}
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return t
		}
		return time.Time{}
	}
	latestByTask := map[string]map[string]interface{}{}
	latestTimeByTask := map[string]time.Time{}

	if b, err := os.ReadFile(auditPath); err == nil {
		lines := strings.Split(string(b), "\n")
		for _, ln := range lines {
			if strings.TrimSpace(ln) == "" {
				continue
			}
			var row map[string]interface{}
			if json.Unmarshal([]byte(ln), &row) != nil {
				continue
			}
			source := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["source"])))
			if source == "heartbeat" {
				continue
			}
			taskID := strings.TrimSpace(fmt.Sprintf("%v", row["task_id"]))
			if taskID == "" {
				continue
			}
			t := parseTime(fmt.Sprintf("%v", row["time"]))
			prev, ok := latestTimeByTask[taskID]
			if ok && !t.IsZero() && t.Before(prev) {
				continue
			}
			latestByTask[taskID] = row
			if !t.IsZero() {
				latestTimeByTask[taskID] = t
			}
		}
	}

	if b, err := os.ReadFile(tasksPath); err == nil {
		var tasks []map[string]interface{}
		if json.Unmarshal(b, &tasks) == nil {
			for _, t := range tasks {
				id := strings.TrimSpace(fmt.Sprintf("%v", t["id"]))
				if id == "" {
					continue
				}
				row := map[string]interface{}{
					"task_id":       id,
					"time":          fmt.Sprintf("%v", t["updated_at"]),
					"status":        fmt.Sprintf("%v", t["status"]),
					"source":        fmt.Sprintf("%v", t["source"]),
					"input_preview": fmt.Sprintf("%v", t["content"]),
					"log":           fmt.Sprintf("%v", t["block_reason"]),
				}
				tm := parseTime(fmt.Sprintf("%v", row["time"]))
				prev, ok := latestTimeByTask[id]
				if !ok || prev.IsZero() || (!tm.IsZero() && tm.After(prev)) {
					latestByTask[id] = row
					if !tm.IsZero() {
						latestTimeByTask[id] = tm
					}
				}
			}
		}
	}

	items := make([]map[string]interface{}, 0, len(latestByTask))
	for _, row := range latestByTask {
		items = append(items, row)
	}
	sort.Slice(items, func(i, j int) bool {
		ti := parseTime(fmt.Sprintf("%v", items[i]["time"]))
		tj := parseTime(fmt.Sprintf("%v", items[j]["time"]))
		if ti.IsZero() && tj.IsZero() {
			return fmt.Sprintf("%v", items[i]["task_id"]) > fmt.Sprintf("%v", items[j]["task_id"])
		}
		if ti.IsZero() {
			return false
		}
		if tj.IsZero() {
			return true
		}
		return ti.After(tj)
	})

	stats := map[string]int{
		"running":    0,
		"waiting":    0,
		"blocked":    0,
		"error":      0,
		"success":    0,
		"suppressed": 0,
	}
	for _, row := range items {
		st := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["status"])))
		if _, ok := stats[st]; ok {
			stats[st]++
		}
	}

	mainState := "idle"
	mainZone := "breakroom"
	switch {
	case stats["error"] > 0 || stats["blocked"] > 0:
		mainState = "error"
		mainZone = "bug"
	case stats["running"] > 0:
		mainState = "executing"
		mainZone = "work"
	case stats["suppressed"] > 0:
		mainState = "syncing"
		mainZone = "server"
	case stats["success"] > 0:
		mainState = "writing"
		mainZone = "work"
	default:
		mainState = "idle"
		mainZone = "breakroom"
	}

	mainTaskID := ""
	mainDetail := "No active task"
	for _, row := range items {
		st := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["status"])))
		if st == "running" || st == "error" || st == "blocked" || st == "waiting" {
			mainTaskID = strings.TrimSpace(fmt.Sprintf("%v", row["task_id"]))
			mainDetail = strings.TrimSpace(fmt.Sprintf("%v", row["input_preview"]))
			if mainDetail == "" {
				mainDetail = strings.TrimSpace(fmt.Sprintf("%v", row["log"]))
			}
			if mainDetail == "" {
				mainDetail = "Task " + mainTaskID
			}
			break
		}
	}
	if mainTaskID == "" && len(items) > 0 {
		mainTaskID = strings.TrimSpace(fmt.Sprintf("%v", items[0]["task_id"]))
		mainDetail = strings.TrimSpace(fmt.Sprintf("%v", items[0]["input_preview"]))
		if mainDetail == "" {
			mainDetail = strings.TrimSpace(fmt.Sprintf("%v", items[0]["log"]))
		}
		if mainDetail == "" {
			mainDetail = "Task " + mainTaskID
		}
	}

	nodeState := func(n NodeInfo) string {
		if !n.Online {
			return "offline"
		}
		// A node that is still online but hasn't heartbeat recently is treated as syncing.
		if !n.LastSeenAt.IsZero() && now.Sub(n.LastSeenAt) > 20*time.Second {
			return "syncing"
		}
		return "online"
	}
	nodeZone := func(n NodeInfo) string {
		if !n.Online {
			return "bug"
		}
		if n.Capabilities.Model || n.Capabilities.Run {
			return "work"
		}
		if n.Capabilities.Invoke || n.Capabilities.Camera || n.Capabilities.Screen || n.Capabilities.Canvas || n.Capabilities.Location {
			return "server"
		}
		return "breakroom"
	}
	nodeDetail := func(n NodeInfo) string {
		parts := make([]string, 0, 4)
		if ep := strings.TrimSpace(n.Endpoint); ep != "" {
			parts = append(parts, ep)
		}
		switch {
		case strings.TrimSpace(n.OS) != "" && strings.TrimSpace(n.Arch) != "":
			parts = append(parts, fmt.Sprintf("%s/%s", strings.TrimSpace(n.OS), strings.TrimSpace(n.Arch)))
		case strings.TrimSpace(n.OS) != "":
			parts = append(parts, strings.TrimSpace(n.OS))
		case strings.TrimSpace(n.Arch) != "":
			parts = append(parts, strings.TrimSpace(n.Arch))
		}
		if m := len(n.Models); m > 0 {
			parts = append(parts, fmt.Sprintf("models:%d", m))
		}
		if !n.LastSeenAt.IsZero() {
			parts = append(parts, "seen:"+n.LastSeenAt.UTC().Format(time.RFC3339))
		}
		if len(parts) == 0 {
			return "node " + strings.TrimSpace(n.ID)
		}
		return strings.Join(parts, " · ")
	}

	allNodes := []NodeInfo{}
	if s.mgr != nil {
		allNodes = s.mgr.List()
	}
	host, _ := os.Hostname()
	localNode := NodeInfo{ID: "local", Name: "local", Endpoint: "gateway", Version: gatewayBuildVersion(), LastSeenAt: now, Online: true}
	if strings.TrimSpace(host) != "" {
		localNode.Name = strings.TrimSpace(host)
	}
	if ip := detectLocalIP(); ip != "" {
		localNode.Endpoint = ip
	}
	hostLower := strings.ToLower(strings.TrimSpace(host))
	mainNode := localNode
	otherNodes := make([]NodeInfo, 0, len(allNodes))
	for _, n := range allNodes {
		idLower := strings.ToLower(strings.TrimSpace(n.ID))
		nameLower := strings.ToLower(strings.TrimSpace(n.Name))
		isLocal := idLower == "local" || nameLower == "local" || (hostLower != "" && nameLower == hostLower)
		if isLocal {
			if strings.TrimSpace(n.Name) != "" {
				mainNode.Name = strings.TrimSpace(n.Name)
			}
			if strings.TrimSpace(localNode.Name) != "" {
				mainNode.Name = strings.TrimSpace(localNode.Name)
			}
			if strings.TrimSpace(n.Endpoint) != "" {
				mainNode.Endpoint = strings.TrimSpace(n.Endpoint)
			}
			if strings.TrimSpace(localNode.Endpoint) != "" {
				mainNode.Endpoint = strings.TrimSpace(localNode.Endpoint)
			}
			if strings.TrimSpace(n.OS) != "" {
				mainNode.OS = strings.TrimSpace(n.OS)
			}
			if strings.TrimSpace(n.Arch) != "" {
				mainNode.Arch = strings.TrimSpace(n.Arch)
			}
			if len(n.Models) > 0 {
				mainNode.Models = append([]string(nil), n.Models...)
			}
			mainNode.Online = true
			mainNode.LastSeenAt = now
			mainNode.Version = localNode.Version
			continue
		}
		otherNodes = append(otherNodes, n)
	}

	onlineNodes := 1 // main(local) is always considered online.
	nodesPayload := make([]map[string]interface{}, 0, 24)
	for _, n := range otherNodes {
		if n.Online {
			onlineNodes++
		}
		id := strings.TrimSpace(n.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(n.Name)
		if name == "" {
			name = id
		}
		updatedAt := ""
		if !n.LastSeenAt.IsZero() {
			updatedAt = n.LastSeenAt.UTC().Format(time.RFC3339)
		}
		nodesPayload = append(nodesPayload, map[string]interface{}{
			"id":         id,
			"name":       name,
			"state":      nodeState(n),
			"zone":       nodeZone(n),
			"detail":     nodeDetail(n),
			"updated_at": updatedAt,
		})
		if len(nodesPayload) >= 24 {
			break
		}
	}

	mainDetailOut := mainDetail
	if nodeInfo := nodeDetail(mainNode); strings.TrimSpace(nodeInfo) != "" {
		if strings.TrimSpace(mainDetailOut) == "" || strings.EqualFold(strings.TrimSpace(mainDetailOut), "No active task") {
			mainDetailOut = nodeInfo
		} else {
			mainDetailOut = mainDetailOut + " · " + nodeInfo
		}
	}

	ekgErr5m := 0
	cutoff := now.Add(-5 * time.Minute)
	for _, row := range s.loadEKGRowsCached(ekgPath, 2000) {
		status := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["status"])))
		if status != "error" {
			continue
		}
		ts := parseTime(fmt.Sprintf("%v", row["time"]))
		if !ts.IsZero() && ts.Before(cutoff) {
			continue
		}
		ekgErr5m++
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":   true,
		"time": now.Format(time.RFC3339),
		"main": map[string]interface{}{
			"id":      mainNode.ID,
			"name":    mainNode.Name,
			"state":   mainState,
			"detail":  mainDetailOut,
			"zone":    mainZone,
			"task_id": mainTaskID,
		},
		"nodes": nodesPayload,
		"stats": map[string]interface{}{
			"running":      stats["running"],
			"waiting":      stats["waiting"],
			"blocked":      stats["blocked"],
			"error":        stats["error"],
			"success":      stats["success"],
			"suppressed":   stats["suppressed"],
			"online_nodes": onlineNodes,
			"ekg_error_5m": ekgErr5m,
		},
	})
}

func (s *RegistryServer) handleWebUITasks(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	tasksPath := filepath.Join(strings.TrimSpace(s.workspacePath), "memory", "tasks.json")
	if r.Method == http.MethodGet {
		b, err := os.ReadFile(tasksPath)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "items": []map[string]interface{}{}})
			return
		}
		var items []map[string]interface{}
		if err := json.Unmarshal(b, &items); err != nil {
			http.Error(w, "invalid tasks file", http.StatusInternalServerError)
			return
		}
		sort.Slice(items, func(i, j int) bool {
			return fmt.Sprintf("%v", items[i]["updated_at"]) > fmt.Sprintf("%v", items[j]["updated_at"])
		})
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "items": items})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	action := fmt.Sprintf("%v", body["action"])
	now := time.Now().UTC().Format(time.RFC3339)
	items := []map[string]interface{}{}
	if b, err := os.ReadFile(tasksPath); err == nil {
		_ = json.Unmarshal(b, &items)
	}
	switch action {
	case "create":
		it, _ := body["item"].(map[string]interface{})
		if it == nil {
			http.Error(w, "item required", http.StatusBadRequest)
			return
		}
		id := fmt.Sprintf("%v", it["id"])
		if id == "" {
			id = fmt.Sprintf("task_%d", time.Now().UnixNano())
		}
		it["id"] = id
		if fmt.Sprintf("%v", it["status"]) == "" {
			it["status"] = "todo"
		}
		if fmt.Sprintf("%v", it["source"]) == "" {
			it["source"] = "manual"
		}
		it["updated_at"] = now
		items = append(items, it)
	case "update":
		id := fmt.Sprintf("%v", body["id"])
		it, _ := body["item"].(map[string]interface{})
		if id == "" || it == nil {
			http.Error(w, "id and item required", http.StatusBadRequest)
			return
		}
		updated := false
		for _, row := range items {
			if fmt.Sprintf("%v", row["id"]) == id {
				for k, v := range it {
					row[k] = v
				}
				row["id"] = id
				row["updated_at"] = now
				updated = true
				break
			}
		}
		if !updated {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}
	case "delete":
		id := fmt.Sprintf("%v", body["id"])
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		filtered := make([]map[string]interface{}, 0, len(items))
		for _, row := range items {
			if fmt.Sprintf("%v", row["id"]) != id {
				filtered = append(filtered, row)
			}
		}
		items = filtered
	default:
		http.Error(w, "unsupported action", http.StatusBadRequest)
		return
	}
	_ = os.MkdirAll(filepath.Dir(tasksPath), 0755)
	out, _ := json.MarshalIndent(items, "", "  ")
	if err := os.WriteFile(tasksPath, out, 0644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
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
