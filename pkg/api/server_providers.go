package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	cfgpkg "github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/providers"
)

func (s *Server) handleWebUIProviderOAuthStart(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Provider       string                `json:"provider"`
		AccountLabel   string                `json:"account_label"`
		NetworkProxy   string                `json:"network_proxy"`
		ProviderConfig cfgpkg.ProviderConfig `json:"provider_config"`
	}
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
	} else {
		body.Provider = strings.TrimSpace(r.URL.Query().Get("provider"))
		body.AccountLabel = strings.TrimSpace(r.URL.Query().Get("account_label"))
		body.NetworkProxy = strings.TrimSpace(r.URL.Query().Get("network_proxy"))
	}
	cfg, pc, err := s.resolveProviderConfig(strings.TrimSpace(body.Provider), body.ProviderConfig)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = cfg
	timeout := pc.TimeoutSec
	if timeout <= 0 {
		timeout = 90
	}
	loginMgr, err := providers.NewOAuthLoginManager(pc, time.Duration(timeout)*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	flow, err := loginMgr.StartManualFlowWithOptions(providers.OAuthLoginOptions{
		AccountLabel: body.AccountLabel,
		NetworkProxy: firstNonEmptyString(strings.TrimSpace(body.NetworkProxy), strings.TrimSpace(pc.OAuth.NetworkProxy)),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	flowID := fmt.Sprintf("%d", time.Now().UnixNano())
	s.oauthFlowMu.Lock()
	s.oauthFlows[flowID] = flow
	s.oauthFlowMu.Unlock()
	writeJSON(w, map[string]interface{}{
		"ok":            true,
		"flow_id":       flowID,
		"mode":          flow.Mode,
		"auth_url":      flow.AuthURL,
		"user_code":     flow.UserCode,
		"instructions":  flow.Instructions,
		"account_label": strings.TrimSpace(body.AccountLabel),
		"network_proxy": strings.TrimSpace(body.NetworkProxy),
	})
}

func (s *Server) handleWebUIProviderOAuthComplete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Provider       string                `json:"provider"`
		FlowID         string                `json:"flow_id"`
		CallbackURL    string                `json:"callback_url"`
		AccountLabel   string                `json:"account_label"`
		NetworkProxy   string                `json:"network_proxy"`
		ProviderConfig cfgpkg.ProviderConfig `json:"provider_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	cfg, pc, err := s.resolveProviderConfig(strings.TrimSpace(body.Provider), body.ProviderConfig)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	timeout := pc.TimeoutSec
	if timeout <= 0 {
		timeout = 90
	}
	loginMgr, err := providers.NewOAuthLoginManager(pc, time.Duration(timeout)*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.oauthFlowMu.Lock()
	flow := s.oauthFlows[strings.TrimSpace(body.FlowID)]
	delete(s.oauthFlows, strings.TrimSpace(body.FlowID))
	s.oauthFlowMu.Unlock()
	if flow == nil {
		http.Error(w, "oauth flow not found", http.StatusBadRequest)
		return
	}
	session, models, err := loginMgr.CompleteManualFlowWithOptions(r.Context(), pc.APIBase, flow, body.CallbackURL, providers.OAuthLoginOptions{
		AccountLabel: strings.TrimSpace(body.AccountLabel),
		NetworkProxy: firstNonEmptyString(strings.TrimSpace(body.NetworkProxy), strings.TrimSpace(pc.OAuth.NetworkProxy)),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if session.CredentialFile != "" {
		pc.OAuth.CredentialFile = session.CredentialFile
		pc.OAuth.CredentialFiles = appendUniqueStrings(pc.OAuth.CredentialFiles, session.CredentialFile)
	}
	if err := s.saveProviderConfig(cfg, body.Provider, pc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":              true,
		"account":         session.Email,
		"credential_file": session.CredentialFile,
		"network_proxy":   session.NetworkProxy,
		"models":          models,
	})
}

func (s *Server) handleWebUIProviderOAuthImport(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	providerName := strings.TrimSpace(r.FormValue("provider"))
	accountLabel := strings.TrimSpace(r.FormValue("account_label"))
	networkProxy := strings.TrimSpace(r.FormValue("network_proxy"))
	inlineCfgRaw := strings.TrimSpace(r.FormValue("provider_config"))
	var inlineCfg cfgpkg.ProviderConfig
	if inlineCfgRaw != "" {
		if err := json.Unmarshal([]byte(inlineCfgRaw), &inlineCfg); err != nil {
			http.Error(w, "invalid provider_config", http.StatusBadRequest)
			return
		}
	}
	cfg, pc, err := s.resolveProviderConfig(providerName, inlineCfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	timeout := pc.TimeoutSec
	if timeout <= 0 {
		timeout = 90
	}
	loginMgr, err := providers.NewOAuthLoginManager(pc, time.Duration(timeout)*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	session, models, err := loginMgr.ImportAuthJSONWithOptions(r.Context(), pc.APIBase, header.Filename, data, providers.OAuthLoginOptions{
		AccountLabel: accountLabel,
		NetworkProxy: firstNonEmptyString(networkProxy, strings.TrimSpace(pc.OAuth.NetworkProxy)),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if session.CredentialFile != "" {
		pc.OAuth.CredentialFile = session.CredentialFile
		pc.OAuth.CredentialFiles = appendUniqueStrings(pc.OAuth.CredentialFiles, session.CredentialFile)
	}
	if err := s.saveProviderConfig(cfg, providerName, pc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":              true,
		"account":         session.Email,
		"credential_file": session.CredentialFile,
		"network_proxy":   session.NetworkProxy,
		"models":          models,
	})
}

func (s *Server) handleWebUIProviderOAuthAccounts(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	providerName := strings.TrimSpace(r.URL.Query().Get("provider"))
	cfg, pc, err := s.loadProviderConfig(providerName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = cfg
	timeout := pc.TimeoutSec
	if timeout <= 0 {
		timeout = 90
	}
	loginMgr, err := providers.NewOAuthLoginManager(pc, time.Duration(timeout)*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		accounts, err := loginMgr.ListAccounts()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "accounts": accounts})
	case http.MethodPost:
		var body struct {
			Action         string `json:"action"`
			CredentialFile string `json:"credential_file"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action := strings.ToLower(strings.TrimSpace(body.Action))
		handler := providerOAuthAccountActionHandlers[action]
		if handler == nil {
			http.Error(w, "unsupported action", http.StatusBadRequest)
			return
		}
		result, err := handler(r.Context(), s, loginMgr, cfg, providerName, &pc, strings.TrimSpace(body.CredentialFile))
		if err != nil {
			status := http.StatusBadRequest
			if action == "delete" && strings.Contains(strings.ToLower(err.Error()), "config") {
				status = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), status)
			return
		}
		writeJSON(w, result)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type providerOAuthAccountActionHandler func(context.Context, *Server, *providers.OAuthLoginManager, *cfgpkg.Config, string, *cfgpkg.ProviderConfig, string) (map[string]interface{}, error)

var providerOAuthAccountActionHandlers = map[string]providerOAuthAccountActionHandler{
	"refresh": func(ctx context.Context, _ *Server, loginMgr *providers.OAuthLoginManager, _ *cfgpkg.Config, _ string, _ *cfgpkg.ProviderConfig, credentialFile string) (map[string]interface{}, error) {
		account, err := loginMgr.RefreshAccount(ctx, credentialFile)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"ok": true, "account": account}, nil
	},
	"delete": func(_ context.Context, srv *Server, loginMgr *providers.OAuthLoginManager, cfg *cfgpkg.Config, providerName string, pc *cfgpkg.ProviderConfig, credentialFile string) (map[string]interface{}, error) {
		if err := loginMgr.DeleteAccount(credentialFile); err != nil {
			return nil, err
		}
		pc.OAuth.CredentialFiles = removeStringItem(pc.OAuth.CredentialFiles, credentialFile)
		if strings.TrimSpace(pc.OAuth.CredentialFile) == strings.TrimSpace(credentialFile) {
			pc.OAuth.CredentialFile = ""
			if len(pc.OAuth.CredentialFiles) > 0 {
				pc.OAuth.CredentialFile = pc.OAuth.CredentialFiles[0]
			}
		}
		if err := srv.saveProviderConfig(cfg, providerName, *pc); err != nil {
			return nil, err
		}
		return map[string]interface{}{"ok": true, "deleted": true}, nil
	},
	"clear_cooldown": func(_ context.Context, _ *Server, loginMgr *providers.OAuthLoginManager, _ *cfgpkg.Config, _ string, _ *cfgpkg.ProviderConfig, credentialFile string) (map[string]interface{}, error) {
		if err := loginMgr.ClearCooldown(credentialFile); err != nil {
			return nil, err
		}
		return map[string]interface{}{"ok": true, "cleared": true}, nil
	},
}

func (s *Server) loadProviderConfig(name string) (*cfgpkg.Config, cfgpkg.ProviderConfig, error) {
	if strings.TrimSpace(s.configPath) == "" {
		return nil, cfgpkg.ProviderConfig{}, fmt.Errorf("config path not set")
	}
	cfg, err := cfgpkg.LoadConfig(s.configPath)
	if err != nil {
		return nil, cfgpkg.ProviderConfig{}, err
	}
	providerName := strings.TrimSpace(name)
	if providerName == "" {
		providerName = cfgpkg.PrimaryProviderName(cfg)
	}
	pc, ok := cfgpkg.ProviderConfigByName(cfg, providerName)
	if !ok {
		return nil, cfgpkg.ProviderConfig{}, fmt.Errorf("provider %q not found", providerName)
	}
	return cfg, pc, nil
}

func (s *Server) loadRuntimeProviderName(name string) (*cfgpkg.Config, string, error) {
	if strings.TrimSpace(s.configPath) == "" {
		return nil, "", fmt.Errorf("config path not set")
	}
	cfg, err := cfgpkg.LoadConfig(s.configPath)
	if err != nil {
		return nil, "", err
	}
	providerName := strings.TrimSpace(name)
	if providerName == "" {
		providerName = cfgpkg.PrimaryProviderName(cfg)
	}
	if !cfgpkg.ProviderExists(cfg, providerName) {
		return nil, "", fmt.Errorf("provider %q not found", providerName)
	}
	return cfg, providerName, nil
}

func (s *Server) resolveProviderConfig(name string, inline cfgpkg.ProviderConfig) (*cfgpkg.Config, cfgpkg.ProviderConfig, error) {
	if hasInlineProviderConfig(inline) {
		cfg, err := cfgpkg.LoadConfig(s.configPath)
		if err != nil {
			return nil, cfgpkg.ProviderConfig{}, err
		}
		return cfg, inline, nil
	}
	return s.loadProviderConfig(name)
}

func hasInlineProviderConfig(pc cfgpkg.ProviderConfig) bool {
	return strings.TrimSpace(pc.APIBase) != "" ||
		strings.TrimSpace(pc.APIKey) != "" ||
		len(pc.Models) > 0 ||
		strings.TrimSpace(pc.Auth) != "" ||
		strings.TrimSpace(pc.OAuth.Provider) != ""
}

func (s *Server) saveProviderConfig(cfg *cfgpkg.Config, name string, pc cfgpkg.ProviderConfig) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	providerName := strings.TrimSpace(name)
	if cfg.Models.Providers == nil {
		cfg.Models.Providers = map[string]cfgpkg.ProviderConfig{}
	}
	cfg.Models.Providers[providerName] = pc
	if err := cfgpkg.SaveConfig(s.configPath, cfg); err != nil {
		return err
	}
	if s.onConfigAfter != nil {
		if err := s.onConfigAfter(); err != nil {
			return err
		}
	} else {
		if err := requestSelfReloadSignal(); err != nil {
			return err
		}
	}
	return nil
}

func appendUniqueStrings(values []string, item string) []string {
	item = strings.TrimSpace(item)
	if item == "" {
		return values
	}
	for _, value := range values {
		if strings.TrimSpace(value) == item {
			return values
		}
	}
	return append(values, item)
}

func removeStringItem(values []string, item string) []string {
	item = strings.TrimSpace(item)
	if item == "" {
		return values
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == item {
			continue
		}
		out = append(out, value)
	}
	return out
}

func atoiDefault(raw string, fallback int) int {
	if value, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
		return value
	}
	return fallback
}
