package providers

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YspCoder/clawgo/pkg/config"
)

const (
	oauthFlowCallback = "callback"
	oauthFlowDevice   = "device"

	oauthStyleForm = "form"
	oauthStyleJSON = "json"

	defaultCodexOAuthProvider       = "codex"
	defaultCodexAuthURL             = "https://auth.openai.com/oauth/authorize"
	defaultCodexTokenURL            = "https://auth.openai.com/oauth/token"
	defaultCodexClientID            = "app_EMoamEEZ73f0CkXaXp7hrann"
	defaultCodexCallbackPort        = 1455
	defaultCodexRedirectPath        = "/auth/callback"
	defaultClaudeOAuthProvider      = "claude"
	defaultClaudeAuthURL            = "https://claude.ai/oauth/authorize"
	defaultClaudeTokenURL           = "https://api.anthropic.com/v1/oauth/token"
	defaultClaudeClientID           = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	defaultClaudeCallbackPort       = 54545
	defaultClaudeRedirectPath       = "/callback"
	defaultAntigravityOAuthProvider = "antigravity"
	defaultAntigravityAuthURL       = "https://accounts.google.com/o/oauth2/v2/auth"
	defaultAntigravityTokenURL      = "https://oauth2.googleapis.com/token"
	defaultAntigravityCallbackPort  = 51121
	defaultAntigravityRedirectPath  = "/oauth-callback"
	defaultGeminiOAuthProvider      = "gemini"
	defaultGeminiAuthURL            = "https://accounts.google.com/o/oauth2/v2/auth"
	defaultGeminiTokenURL           = "https://oauth2.googleapis.com/token"
	defaultGeminiCallbackPort       = 8085
	defaultGeminiRedirectPath       = "/oauth2callback"
	defaultKimiOAuthProvider        = "kimi"
	defaultKimiDeviceCodeURL        = "https://auth.kimi.com/api/oauth/device_authorization"
	defaultKimiTokenURL             = "https://auth.kimi.com/api/oauth/token"
	defaultKimiClientID             = "17e5f671-d194-4dfb-9706-5516cb48c098"
	defaultIFlowOAuthProvider       = "iflow"
	defaultIFlowAuthURL             = "https://iflow.cn/oauth"
	defaultIFlowTokenURL            = "https://iflow.cn/oauth/token"
	defaultIFlowClientID            = "10009311001"
	defaultIFlowClientSecret        = "4Z3YjXycVsQvyGF1etiNlIBB4RsqSDtW"
	defaultIFlowCallbackPort        = 11451
	defaultIFlowRedirectPath        = "/oauth2callback"
	defaultQwenOAuthProvider        = "qwen"
	defaultQwenDeviceCodeURL        = "https://chat.qwen.ai/api/v1/oauth2/device/code"
	defaultQwenTokenURL             = "https://chat.qwen.ai/api/v1/oauth2/token"
	defaultQwenClientID             = "f0304373b74a44d2b584a3fb70ca9e56"
)

var (
	defaultCodexScopes  = []string{"openid", "email", "profile", "offline_access"}
	defaultClaudeScopes = []string{"org:create_api_key", "user:profile", "user:inference"}
	defaultGoogleScopes = []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
	}
	defaultAntigravityScopes = []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
		"https://www.googleapis.com/auth/cclog",
		"https://www.googleapis.com/auth/experimentsandconfigs",
	}
	defaultQwenScopes = []string{"openid", "profile", "email", "model.completion"}
)

var (
	defaultAntigravityClientIDValue = "1071006060591-" + "tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	defaultAntigravityClientSecretValue = "GOCSPX-" + "K58FWR486LdLJ1mLB8sXC4z6qDAf"
	defaultGeminiClientIDValue = "681255809395-" + "oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	defaultGeminiClientSecretValue = "GOCSPX-" + "4uHgMPm-1o7Sk-geV6Cu5clXFsxl"
	defaultAntigravityClientID     = firstNonEmpty(strings.TrimSpace(os.Getenv("CLAWGO_ANTIGRAVITY_CLIENT_ID")), defaultAntigravityClientIDValue)
	defaultAntigravityClientSecret = firstNonEmpty(strings.TrimSpace(os.Getenv("CLAWGO_ANTIGRAVITY_CLIENT_SECRET")), defaultAntigravityClientSecretValue)
	defaultGeminiClientID          = firstNonEmpty(strings.TrimSpace(os.Getenv("CLAWGO_GEMINI_CLIENT_ID")), defaultGeminiClientIDValue)
	defaultGeminiClientSecret      = firstNonEmpty(strings.TrimSpace(os.Getenv("CLAWGO_GEMINI_CLIENT_SECRET")), defaultGeminiClientSecretValue)
)

var (
	defaultCodexRefreshLead        = 5 * 24 * time.Hour
	defaultClaudeRefreshLead       = 4 * time.Hour
	defaultAntigravityRefreshLead  = 5 * time.Minute
	defaultKimiRefreshLead         = 5 * time.Minute
	defaultQwenRefreshLead         = 3 * time.Hour
	defaultAntigravityUserInfoURL  = "https://www.googleapis.com/oauth2/v1/userinfo?alt=json"
	defaultGeminiUserInfoURL       = "https://www.googleapis.com/oauth2/v1/userinfo?alt=json"
	defaultAntigravityAPIEndpoint  = "https://cloudcode-pa.googleapis.com"
	defaultAntigravityAPIVersion   = "v1internal"
	defaultAntigravityAPIUserAgent = "google-api-nodejs-client/9.15.1"
	defaultAntigravityAPIClient    = "google-cloud-sdk vscode_cloudshelleditor/0.1"
	defaultAntigravityClientMeta   = `{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}`
)

type oauthSession struct {
	Provider      string         `json:"provider"`
	Type          string         `json:"type,omitempty"`
	AccessToken   string         `json:"access_token"`
	RefreshToken  string         `json:"refresh_token"`
	IDToken       string         `json:"id_token,omitempty"`
	TokenType     string         `json:"token_type,omitempty"`
	AccountID     string         `json:"account_id,omitempty"`
	Email         string         `json:"email,omitempty"`
	Expire        string         `json:"expire,omitempty"`
	LastRefresh   string         `json:"last_refresh,omitempty"`
	Models        []string       `json:"models,omitempty"`
	ProjectID     string         `json:"project_id,omitempty"`
	DeviceID      string         `json:"device_id,omitempty"`
	ResourceURL   string         `json:"resource_url,omitempty"`
	NetworkProxy  string         `json:"network_proxy,omitempty"`
	Scope         string         `json:"scope,omitempty"`
	Token         map[string]any `json:"token,omitempty"`
	Disabled      bool           `json:"disabled,omitempty"`
	DisableReason string         `json:"disable_reason,omitempty"`
	CooldownUntil string         `json:"-"`
	FailureCount  int            `json:"-"`
	LastFailure   string         `json:"-"`
	HealthScore   int            `json:"-"`
	FilePath      string         `json:"-"`
}

type oauthConfig struct {
	Provider        string
	CredentialFile  string
	CredentialFiles []string
	CallbackPort    int
	ClientID        string
	ClientSecret    string
	AuthURL         string
	TokenURL        string
	DeviceCodeURL   string
	UserInfoURL     string
	RedirectURL     string
	RedirectPath    string
	Scopes          []string
	RefreshScan     time.Duration
	RefreshLead     time.Duration
	Cooldown        time.Duration
	FlowKind        string
	TokenStyle      string
	DeviceGrantType string
	DevicePollMax   time.Duration
}

type oauthManager struct {
	providerName string
	cfg          oauthConfig
	timeout      time.Duration
	httpClient   *http.Client
	clientMu     sync.Mutex
	clients      map[string]*http.Client
	mu           sync.Mutex
	cached       []*oauthSession
	cooldowns    map[string]time.Time
	bgCtx        context.Context
	bgCancel     context.CancelFunc
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	ResourceURL  string `json:"resource_url"`
	Scope        string `json:"scope"`
	Account      struct {
		UUID         string `json:"uuid"`
		EmailAddress string `json:"email_address"`
	} `json:"account"`
	Organization struct {
		UUID string `json:"uuid"`
		Name string `json:"name"`
	} `json:"organization"`
}

type oauthDeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type OAuthLoginManager struct {
	manager *oauthManager
}

type OAuthLoginOptions struct {
	Manual       bool
	NoBrowser    bool
	Reader       io.Reader
	AccountLabel string
	NetworkProxy string
}

type OAuthPendingFlow struct {
	Mode         string `json:"mode,omitempty"`
	State        string `json:"state,omitempty"`
	PKCEVerifier string `json:"pkce_verifier,omitempty"`
	AuthURL      string `json:"auth_url,omitempty"`
	UserCode     string `json:"user_code,omitempty"`
	Instructions string `json:"instructions,omitempty"`
	DeviceCode   string `json:"device_code,omitempty"`
	IntervalSec  int    `json:"interval_sec,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
}

type OAuthSessionInfo struct {
	Email          string
	AccountID      string
	CredentialFile string
	ProjectID      string
	AccountLabel   string
	NetworkProxy   string
}

type OAuthAccountInfo struct {
	Email          string `json:"email"`
	AccountID      string `json:"account_id"`
	CredentialFile string `json:"credential_file"`
	Expire         string `json:"expire,omitempty"`
	LastRefresh    string `json:"last_refresh,omitempty"`
	ProjectID      string `json:"project_id,omitempty"`
	AccountLabel   string `json:"account_label,omitempty"`
	DeviceID       string `json:"device_id,omitempty"`
	ResourceURL    string `json:"resource_url,omitempty"`
	NetworkProxy   string `json:"network_proxy,omitempty"`
	Disabled       bool   `json:"disabled,omitempty"`
	DisableReason  string `json:"disable_reason,omitempty"`
	CooldownUntil  string `json:"cooldown_until,omitempty"`
	FailureCount   int    `json:"failure_count,omitempty"`
	LastFailure    string `json:"last_failure,omitempty"`
	HealthScore    int    `json:"health_score,omitempty"`
	PlanType       string `json:"plan_type,omitempty"`
	QuotaSource    string `json:"quota_source,omitempty"`
	BalanceLabel   string `json:"balance_label,omitempty"`
	BalanceDetail  string `json:"balance_detail,omitempty"`
	SubActiveStart string `json:"subscription_active_start,omitempty"`
	SubActiveUntil string `json:"subscription_active_until,omitempty"`
}

type oauthAttempt struct {
	Session *oauthSession
	Token   string
}

type oauthFailureReason string

const (
	oauthFailureQuota     oauthFailureReason = "quota"
	oauthFailureRateLimit oauthFailureReason = "rate_limit"
	oauthFailureForbidden oauthFailureReason = "forbidden"
	oauthFailureRevoked   oauthFailureReason = "token_revoked"
	oauthFailureDisabled  oauthFailureReason = "deactivated_workspace"
)

type oauthCallbackResult struct {
	Code  string
	State string
	Err   string
}

func NewOAuthLoginManager(pc config.ProviderConfig, timeout time.Duration) (*OAuthLoginManager, error) {
	manager, err := newOAuthManager(pc, timeout)
	if err != nil {
		return nil, err
	}
	return &OAuthLoginManager{manager: manager}, nil
}

func (m *OAuthLoginManager) Login(ctx context.Context, apiBase string, opts OAuthLoginOptions) (*OAuthSessionInfo, []string, error) {
	if m == nil || m.manager == nil {
		return nil, nil, fmt.Errorf("oauth login manager not configured")
	}
	session, models, err := m.manager.login(ctx, apiBase, opts)
	if err != nil {
		return nil, nil, err
	}
	return &OAuthSessionInfo{
		Email:          session.Email,
		AccountID:      session.AccountID,
		CredentialFile: session.FilePath,
		ProjectID:      session.ProjectID,
		AccountLabel:   sessionLabel(session),
		NetworkProxy:   maskedProxyURL(session.NetworkProxy),
	}, models, nil
}

func (m *OAuthLoginManager) CredentialFile() string {
	if m == nil || m.manager == nil {
		return ""
	}
	return m.manager.cfg.CredentialFile
}

func (m *OAuthLoginManager) StartManualFlow() (*OAuthPendingFlow, error) {
	return m.StartManualFlowWithOptions(OAuthLoginOptions{})
}

func (m *OAuthLoginManager) StartManualFlowWithOptions(opts OAuthLoginOptions) (*OAuthPendingFlow, error) {
	if m == nil || m.manager == nil {
		return nil, fmt.Errorf("oauth login manager not configured")
	}
	if m.manager.cfg.FlowKind == oauthFlowDevice {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return m.manager.startDeviceFlow(ctx, opts)
	}
	if _, err := normalizeOptionalProxyURL(opts.NetworkProxy); err != nil {
		return nil, err
	}
	pkceVerifier, pkceChallenge, err := generatePKCE()
	if err != nil {
		return nil, err
	}
	state, err := randomURLToken(24)
	if err != nil {
		return nil, err
	}
	return &OAuthPendingFlow{
		Mode:         oauthFlowCallback,
		State:        state,
		PKCEVerifier: pkceVerifier,
		AuthURL:      m.manager.authorizationURL(state, pkceChallenge),
		Instructions: "Open the authorization URL, finish login, then paste the final callback URL.",
	}, nil
}

func (m *OAuthLoginManager) CompleteManualFlow(ctx context.Context, apiBase string, flow *OAuthPendingFlow, callbackURL string) (*OAuthSessionInfo, []string, error) {
	return m.CompleteManualFlowWithOptions(ctx, apiBase, flow, callbackURL, OAuthLoginOptions{})
}

func (m *OAuthLoginManager) CompleteManualFlowWithOptions(ctx context.Context, apiBase string, flow *OAuthPendingFlow, callbackURL string, opts OAuthLoginOptions) (*OAuthSessionInfo, []string, error) {
	if m == nil || m.manager == nil {
		return nil, nil, fmt.Errorf("oauth login manager not configured")
	}
	if flow == nil {
		return nil, nil, fmt.Errorf("oauth flow is nil")
	}
	var (
		session *oauthSession
		models  []string
		err     error
	)
	if flow.Mode == oauthFlowDevice {
		session, models, err = m.manager.completeDeviceFlow(ctx, apiBase, flow, opts)
	} else {
		callback, parseErr := parseOAuthCallbackURL(callbackURL)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		if callback.State != flow.State {
			return nil, nil, fmt.Errorf("oauth callback state mismatch")
		}
		session, models, err = m.manager.completeLogin(ctx, apiBase, flow.PKCEVerifier, callback, flow.State, opts)
	}
	if err != nil {
		return nil, nil, err
	}
	return &OAuthSessionInfo{
		Email:          session.Email,
		AccountID:      session.AccountID,
		CredentialFile: session.FilePath,
		ProjectID:      session.ProjectID,
		AccountLabel:   sessionLabel(session),
		NetworkProxy:   maskedProxyURL(session.NetworkProxy),
	}, models, nil
}

func (m *OAuthLoginManager) ImportAuthJSON(ctx context.Context, apiBase string, fileName string, data []byte) (*OAuthSessionInfo, []string, error) {
	return m.ImportAuthJSONWithOptions(ctx, apiBase, fileName, data, OAuthLoginOptions{})
}

func (m *OAuthLoginManager) ImportAuthJSONWithOptions(ctx context.Context, apiBase string, fileName string, data []byte, opts OAuthLoginOptions) (*OAuthSessionInfo, []string, error) {
	if m == nil || m.manager == nil {
		return nil, nil, fmt.Errorf("oauth login manager not configured")
	}
	session, models, err := m.manager.importSession(ctx, apiBase, fileName, data, opts)
	if err != nil {
		return nil, nil, err
	}
	return &OAuthSessionInfo{
		Email:          session.Email,
		AccountID:      session.AccountID,
		CredentialFile: session.FilePath,
		ProjectID:      session.ProjectID,
		AccountLabel:   sessionLabel(session),
		NetworkProxy:   maskedProxyURL(session.NetworkProxy),
	}, models, nil
}

func (m *OAuthLoginManager) ListAccounts() ([]OAuthAccountInfo, error) {
	if m == nil || m.manager == nil {
		return nil, fmt.Errorf("oauth login manager not configured")
	}
	m.manager.mu.Lock()
	defer m.manager.mu.Unlock()
	sessions, err := m.manager.loadAllLocked()
	if err != nil {
		return nil, err
	}
	out := make([]OAuthAccountInfo, 0, len(sessions))
	for _, session := range sessions {
		if session == nil {
			continue
		}
		out = append(out, buildOAuthAccountInfo(session))
	}
	return out, nil
}

func (m *OAuthLoginManager) RefreshAccount(ctx context.Context, credentialFile string) (*OAuthAccountInfo, error) {
	if m == nil || m.manager == nil {
		return nil, fmt.Errorf("oauth login manager not configured")
	}
	m.manager.mu.Lock()
	defer m.manager.mu.Unlock()
	sessions, err := m.manager.loadAllLocked()
	if err != nil {
		return nil, err
	}
	for _, session := range sessions {
		if session == nil || strings.TrimSpace(session.FilePath) != strings.TrimSpace(credentialFile) {
			continue
		}
		refreshed, err := m.manager.refreshSessionLocked(ctx, session)
		if err != nil {
			return nil, err
		}
		info := buildOAuthAccountInfo(refreshed)
		return &info, nil
	}
	return nil, fmt.Errorf("oauth credential not found")
}

func buildOAuthAccountInfo(session *oauthSession) OAuthAccountInfo {
	planType, quotaSource, balanceLabel, balanceDetail, subActiveStart, subActiveUntil := extractOAuthBalanceMetadata(session)
	return OAuthAccountInfo{
		Email:          session.Email,
		AccountID:      session.AccountID,
		CredentialFile: session.FilePath,
		Expire:         session.Expire,
		LastRefresh:    session.LastRefresh,
		ProjectID:      session.ProjectID,
		AccountLabel:   sessionLabel(session),
		DeviceID:       session.DeviceID,
		ResourceURL:    session.ResourceURL,
		NetworkProxy:   maskedProxyURL(session.NetworkProxy),
		Disabled:       session.Disabled,
		DisableReason:  session.DisableReason,
		CooldownUntil:  session.CooldownUntil,
		FailureCount:   session.FailureCount,
		LastFailure:    session.LastFailure,
		HealthScore:    sessionHealthScore(session),
		PlanType:       planType,
		QuotaSource:    quotaSource,
		BalanceLabel:   balanceLabel,
		BalanceDetail:  balanceDetail,
		SubActiveStart: subActiveStart,
		SubActiveUntil: subActiveUntil,
	}
}

func (m *OAuthLoginManager) DeleteAccount(credentialFile string) error {
	if m == nil || m.manager == nil {
		return fmt.Errorf("oauth login manager not configured")
	}
	path := strings.TrimSpace(credentialFile)
	if path == "" {
		return fmt.Errorf("oauth credential file is empty")
	}
	m.manager.mu.Lock()
	defer m.manager.mu.Unlock()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	filtered := make([]*oauthSession, 0, len(m.manager.cached))
	for _, session := range m.manager.cached {
		if session == nil || strings.TrimSpace(session.FilePath) == path {
			continue
		}
		filtered = append(filtered, session)
	}
	m.manager.cached = filtered
	delete(m.manager.cooldowns, path)
	files := make([]string, 0, len(m.manager.cfg.CredentialFiles))
	for _, file := range m.manager.cfg.CredentialFiles {
		if strings.TrimSpace(file) == path {
			continue
		}
		files = append(files, file)
	}
	m.manager.cfg.CredentialFiles = files
	if len(files) > 0 {
		m.manager.cfg.CredentialFile = files[0]
	}
	return nil
}

func (m *OAuthLoginManager) ClearCooldown(credentialFile string) error {
	if m == nil || m.manager == nil {
		return fmt.Errorf("oauth login manager not configured")
	}
	path := strings.TrimSpace(credentialFile)
	if path == "" {
		return fmt.Errorf("oauth credential file is empty")
	}
	m.manager.mu.Lock()
	defer m.manager.mu.Unlock()
	delete(m.manager.cooldowns, path)
	for _, session := range m.manager.cached {
		if session != nil && strings.TrimSpace(session.FilePath) == path {
			session.CooldownUntil = ""
			recordProviderRuntimeChange(m.manager.providerName, "oauth", firstNonEmpty(session.Email, session.AccountID, session.FilePath), "manual_clear_oauth_cooldown", "oauth cooldown cleared from runtime panel")
		}
	}
	return nil
}

func newOAuthManager(pc config.ProviderConfig, timeout time.Duration) (*oauthManager, error) {
	resolved, err := resolveOAuthConfig(pc)
	if err != nil {
		return nil, err
	}
	client, err := newOAuthHTTPClient(resolved.Provider, timeout, "")
	if err != nil {
		return nil, err
	}
	bgCtx, bgCancel := context.WithCancel(context.Background())
	manager := &oauthManager{
		cfg:        resolved,
		timeout:    timeout,
		httpClient: client,
		clients:    map[string]*http.Client{"": client},
		cooldowns:  map[string]time.Time{},
		bgCtx:      bgCtx,
		bgCancel:   bgCancel,
	}
	manager.startBackgroundRefreshLoop()
	return manager, nil
}

func newOAuthHTTPClient(provider string, timeout time.Duration, proxyURL string) (*http.Client, error) {
	normalizedProxy, err := normalizeOptionalProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}
	if provider == defaultClaudeOAuthProvider {
		return newAnthropicOAuthHTTPClient(timeout, normalizedProxy)
	}
	if normalizedProxy == "" {
		return &http.Client{Timeout: timeout}, nil
	}
	parsed, err := url.Parse(normalizedProxy)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyURL(parsed),
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
	}, nil
}

func resolveOAuthConfig(pc config.ProviderConfig) (oauthConfig, error) {
	provider := strings.ToLower(strings.TrimSpace(pc.OAuth.Provider))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(pc.Auth))
	}
	if provider == "oauth" || provider == "" {
		return oauthConfig{}, fmt.Errorf("oauth provider is required")
	}
	provider = normalizeOAuthProvider(provider)
	cfg := oauthConfig{
		Provider:        provider,
		CredentialFile:  strings.TrimSpace(pc.OAuth.CredentialFile),
		CredentialFiles: trimNonEmptyStrings(pc.OAuth.CredentialFiles),
		CallbackPort:    pc.OAuth.CallbackPort,
		ClientID:        strings.TrimSpace(pc.OAuth.ClientID),
		ClientSecret:    strings.TrimSpace(pc.OAuth.ClientSecret),
		AuthURL:         strings.TrimSpace(pc.OAuth.AuthURL),
		TokenURL:        strings.TrimSpace(pc.OAuth.TokenURL),
		RedirectURL:     strings.TrimSpace(pc.OAuth.RedirectURL),
		Scopes:          trimNonEmptyStrings(pc.OAuth.Scopes),
		Cooldown:        durationFromSeconds(pc.OAuth.CooldownSec, 15*time.Minute),
		RefreshScan:     durationFromSeconds(pc.OAuth.RefreshScanSec, 10*time.Minute),
		RefreshLead:     defaultRefreshLead(provider, pc.OAuth.RefreshLeadSec),
		DeviceGrantType: "urn:ietf:params:oauth:grant-type:device_code",
		DevicePollMax:   15 * time.Minute,
		TokenStyle:      oauthStyleForm,
		FlowKind:        oauthFlowCallback,
	}
	switch provider {
	case defaultCodexOAuthProvider:
		cfg.CallbackPort = defaultInt(cfg.CallbackPort, defaultCodexCallbackPort)
		cfg.ClientID = firstNonEmpty(cfg.ClientID, defaultCodexClientID)
		cfg.AuthURL = firstNonEmpty(cfg.AuthURL, defaultCodexAuthURL)
		cfg.TokenURL = firstNonEmpty(cfg.TokenURL, defaultCodexTokenURL)
		cfg.RedirectPath = defaultCodexRedirectPath
		if len(cfg.Scopes) == 0 {
			cfg.Scopes = append([]string(nil), defaultCodexScopes...)
		}
	case defaultClaudeOAuthProvider:
		cfg.CallbackPort = defaultInt(cfg.CallbackPort, defaultClaudeCallbackPort)
		cfg.ClientID = firstNonEmpty(cfg.ClientID, defaultClaudeClientID)
		cfg.AuthURL = firstNonEmpty(cfg.AuthURL, defaultClaudeAuthURL)
		cfg.TokenURL = firstNonEmpty(cfg.TokenURL, defaultClaudeTokenURL)
		cfg.RedirectPath = defaultClaudeRedirectPath
		cfg.TokenStyle = oauthStyleJSON
		if len(cfg.Scopes) == 0 {
			cfg.Scopes = append([]string(nil), defaultClaudeScopes...)
		}
	case defaultAntigravityOAuthProvider:
		cfg.CallbackPort = defaultInt(cfg.CallbackPort, defaultAntigravityCallbackPort)
		cfg.ClientID = firstNonEmpty(cfg.ClientID, defaultAntigravityClientID)
		cfg.ClientSecret = firstNonEmpty(cfg.ClientSecret, defaultAntigravityClientSecret)
		cfg.AuthURL = firstNonEmpty(cfg.AuthURL, defaultAntigravityAuthURL)
		cfg.TokenURL = firstNonEmpty(cfg.TokenURL, defaultAntigravityTokenURL)
		cfg.UserInfoURL = firstNonEmpty(cfg.UserInfoURL, defaultAntigravityUserInfoURL)
		cfg.RedirectPath = defaultAntigravityRedirectPath
		if len(cfg.Scopes) == 0 {
			cfg.Scopes = append([]string(nil), defaultAntigravityScopes...)
		}
	case defaultGeminiOAuthProvider:
		cfg.CallbackPort = defaultInt(cfg.CallbackPort, defaultGeminiCallbackPort)
		cfg.ClientID = firstNonEmpty(cfg.ClientID, defaultGeminiClientID)
		cfg.ClientSecret = firstNonEmpty(cfg.ClientSecret, defaultGeminiClientSecret)
		cfg.AuthURL = firstNonEmpty(cfg.AuthURL, defaultGeminiAuthURL)
		cfg.TokenURL = firstNonEmpty(cfg.TokenURL, defaultGeminiTokenURL)
		cfg.UserInfoURL = firstNonEmpty(cfg.UserInfoURL, defaultGeminiUserInfoURL)
		cfg.RedirectPath = defaultGeminiRedirectPath
		if len(cfg.Scopes) == 0 {
			cfg.Scopes = append([]string(nil), defaultGoogleScopes...)
		}
	case defaultKimiOAuthProvider:
		cfg.FlowKind = oauthFlowDevice
		cfg.ClientID = firstNonEmpty(cfg.ClientID, defaultKimiClientID)
		cfg.DeviceCodeURL = firstNonEmpty(strings.TrimSpace(pc.OAuth.AuthURL), defaultKimiDeviceCodeURL)
		cfg.TokenURL = firstNonEmpty(cfg.TokenURL, defaultKimiTokenURL)
		cfg.AuthURL = cfg.DeviceCodeURL
	case defaultIFlowOAuthProvider:
		cfg.CallbackPort = defaultInt(cfg.CallbackPort, defaultIFlowCallbackPort)
		cfg.ClientID = firstNonEmpty(cfg.ClientID, defaultIFlowClientID)
		cfg.ClientSecret = firstNonEmpty(cfg.ClientSecret, defaultIFlowClientSecret)
		cfg.AuthURL = firstNonEmpty(cfg.AuthURL, defaultIFlowAuthURL)
		cfg.TokenURL = firstNonEmpty(cfg.TokenURL, defaultIFlowTokenURL)
		cfg.RedirectPath = defaultIFlowRedirectPath
	case defaultQwenOAuthProvider:
		cfg.FlowKind = oauthFlowDevice
		cfg.ClientID = firstNonEmpty(cfg.ClientID, defaultQwenClientID)
		cfg.DeviceCodeURL = firstNonEmpty(strings.TrimSpace(pc.OAuth.AuthURL), defaultQwenDeviceCodeURL)
		cfg.TokenURL = firstNonEmpty(cfg.TokenURL, defaultQwenTokenURL)
		cfg.AuthURL = cfg.DeviceCodeURL
		if len(cfg.Scopes) == 0 {
			cfg.Scopes = append([]string(nil), defaultQwenScopes...)
		}
	default:
		return oauthConfig{}, fmt.Errorf("unsupported oauth provider %q", provider)
	}
	if cfg.FlowKind == oauthFlowCallback && cfg.RedirectURL == "" {
		cfg.RedirectURL = fmt.Sprintf("http://localhost:%d%s", cfg.CallbackPort, cfg.RedirectPath)
	}
	if cfg.CredentialFile == "" {
		cfg.CredentialFile = filepath.Join(config.GetConfigDir(), "auth", provider+".json")
	}
	if len(cfg.CredentialFiles) == 0 {
		cfg.CredentialFiles = []string{cfg.CredentialFile}
	} else {
		cfg.CredentialFiles = uniqueStrings(append([]string{cfg.CredentialFile}, cfg.CredentialFiles...))
		cfg.CredentialFile = cfg.CredentialFiles[0]
	}
	return cfg, nil
}

func normalizeOAuthProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic", "claude-code", "claude_code", "claude-api-key", "claude_api_key":
		return defaultClaudeOAuthProvider
	case "gemini-cli", "geminicli", "gemini_cli", "google", "gemini-api-key", "gemini_api_key":
		return defaultGeminiOAuthProvider
	case "aistudio", "ai-studio", "ai_studio", "google-ai-studio", "google_ai_studio", "googleaistudio":
		return "aistudio"
	case "openai-compatibility", "openai_compatibility", "openai-compat", "openai_compat":
		return "openai-compatibility"
	case "vertex-api-key", "vertex_api_key", "vertex-compat", "vertex_compat", "vertex-compatibility", "vertex_compatibility":
		return "vertex"
	case "codex-api-key", "codex_api_key":
		return defaultCodexOAuthProvider
	case "i-flow", "i_flow":
		return defaultIFlowOAuthProvider
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func defaultRefreshLead(provider string, overrideSec int) time.Duration {
	if overrideSec > 0 {
		return time.Duration(overrideSec) * time.Second
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case defaultCodexOAuthProvider:
		return defaultCodexRefreshLead
	case defaultClaudeOAuthProvider:
		return defaultClaudeRefreshLead
	case defaultAntigravityOAuthProvider:
		return defaultAntigravityRefreshLead
	case defaultKimiOAuthProvider:
		return defaultKimiRefreshLead
	case defaultIFlowOAuthProvider:
		return 30 * time.Minute
	case defaultQwenOAuthProvider:
		return defaultQwenRefreshLead
	default:
		return 30 * time.Minute
	}
}

func (m *oauthManager) models(ctx context.Context, apiBase string) ([]string, error) {
	attempts, err := m.prepareAttemptsLocked(ctx)
	if err != nil {
		return nil, err
	}
	if len(attempts) == 0 {
		return nil, fmt.Errorf("oauth session not found, run `clawgo provider login` first")
	}
	var merged []string
	seen := map[string]struct{}{}
	var lastErr error
	for _, attempt := range attempts {
		client, err := m.httpClientForSession(attempt.Session)
		if err != nil {
			lastErr = err
			continue
		}
		models, err := fetchOpenAIModels(ctx, client, apiBase, attempt.Token)
		if err != nil {
			lastErr = err
			continue
		}
		attempt.Session.Models = append([]string(nil), models...)
		_ = m.persistSessionLocked(attempt.Session)
		for _, model := range models {
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			merged = append(merged, model)
		}
	}
	if len(merged) > 0 {
		return merged, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no oauth sessions available")
}

func (m *oauthManager) login(ctx context.Context, apiBase string, opts OAuthLoginOptions) (*oauthSession, []string, error) {
	if m == nil {
		return nil, nil, fmt.Errorf("oauth manager not configured")
	}
	if m.cfg.FlowKind == oauthFlowDevice {
		flow, err := m.startDeviceFlow(ctx, opts)
		if err != nil {
			return nil, nil, err
		}
		fmt.Printf("Open this URL to continue OAuth login:\n%s\n", flow.AuthURL)
		if strings.TrimSpace(flow.UserCode) != "" {
			fmt.Printf("User code: %s\n", flow.UserCode)
		}
		if !opts.NoBrowser {
			if err := openBrowser(flow.AuthURL); err != nil {
				fmt.Printf("Automatic browser open failed: %v\n", err)
			}
		}
		return m.completeDeviceFlow(ctx, apiBase, flow, opts)
	}
	pkceVerifier, pkceChallenge, err := generatePKCE()
	if err != nil {
		return nil, nil, err
	}
	state, err := randomURLToken(24)
	if err != nil {
		return nil, nil, err
	}
	callback, err := m.obtainOAuthCallback(ctx, state, pkceChallenge, opts)
	if err != nil {
		return nil, nil, err
	}
	if callback.State != state {
		return nil, nil, fmt.Errorf("oauth callback state mismatch")
	}
	if callback.Err != "" {
		return nil, nil, fmt.Errorf("oauth callback returned error: %s", callback.Err)
	}
	return m.completeLogin(ctx, apiBase, pkceVerifier, callback, state, opts)
}

func (m *oauthManager) completeLogin(ctx context.Context, apiBase, pkceVerifier string, callback *oauthCallbackResult, state string, opts OAuthLoginOptions) (*oauthSession, []string, error) {
	session, err := m.exchangeCode(ctx, callback.Code, pkceVerifier, state, opts.NetworkProxy)
	if err != nil {
		return nil, nil, err
	}
	if err := m.applySessionOptions(session, opts); err != nil {
		return nil, nil, err
	}
	client, err := m.httpClientForSession(session)
	if err != nil {
		return nil, nil, err
	}
	models, _ := fetchOpenAIModels(ctx, client, apiBase, session.AccessToken)
	if len(models) > 0 {
		session.Models = append([]string(nil), models...)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	path, err := m.allocateCredentialPathLocked(session)
	if err != nil {
		return nil, nil, err
	}
	session.FilePath = path
	if err := m.persistSessionLocked(session); err != nil {
		return nil, nil, err
	}
	m.cached = appendLoadedSession(m.cached, session)
	m.cfg.CredentialFiles = uniqueStrings(append(m.cfg.CredentialFiles, path))
	m.cfg.CredentialFile = m.cfg.CredentialFiles[0]
	return session, models, nil
}

func (m *oauthManager) importSession(ctx context.Context, apiBase, fileName string, data []byte, opts OAuthLoginOptions) (*oauthSession, []string, error) {
	session, err := parseImportedOAuthSession(m.cfg.Provider, fileName, data)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(session.AccessToken) == "" && strings.TrimSpace(session.RefreshToken) == "" {
		return nil, nil, fmt.Errorf("auth.json missing access_token/refresh_token")
	}
	if strings.TrimSpace(session.AccessToken) == "" && strings.TrimSpace(session.RefreshToken) != "" {
		refreshed, refreshErr := m.refreshImportedSession(ctx, session)
		if refreshErr != nil {
			return nil, nil, refreshErr
		}
		session = refreshed
	}
	session, err = m.enrichSession(ctx, session)
	if err != nil {
		return nil, nil, err
	}
	if err := m.applySessionOptions(session, opts); err != nil {
		return nil, nil, err
	}
	client, err := m.httpClientForSession(session)
	if err != nil {
		return nil, nil, err
	}
	models, _ := fetchOpenAIModels(ctx, client, apiBase, session.AccessToken)
	if len(models) > 0 {
		session.Models = append([]string(nil), models...)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	path, err := m.allocateCredentialPathLocked(session)
	if err != nil {
		return nil, nil, err
	}
	session.FilePath = path
	if err := m.persistSessionLocked(session); err != nil {
		return nil, nil, err
	}
	m.cached = appendLoadedSession(m.cached, session)
	m.cfg.CredentialFiles = uniqueStrings(append(m.cfg.CredentialFiles, path))
	m.cfg.CredentialFile = m.cfg.CredentialFiles[0]
	return session, models, nil
}

func (m *oauthManager) refreshImportedSession(ctx context.Context, session *oauthSession) (*oauthSession, error) {
	if session == nil {
		return nil, fmt.Errorf("oauth session is nil")
	}
	return m.refreshSessionData(ctx, session)
}

func (m *oauthManager) prepareAttemptsLocked(ctx context.Context) ([]oauthAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessions, err := m.loadAllLocked()
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}
	attempts := make([]oauthAttempt, 0, len(sessions))
	for _, session := range sessions {
		if session == nil || strings.TrimSpace(session.AccessToken) == "" {
			continue
		}
		if session.Disabled {
			continue
		}
		if m.sessionOnCooldown(session) {
			continue
		}
		if sessionNeedsRefresh(session, time.Minute) && strings.TrimSpace(session.RefreshToken) != "" {
			refreshed, err := m.refreshSessionLocked(ctx, session)
			if err == nil {
				session = refreshed
			}
		}
		token := strings.TrimSpace(session.AccessToken)
		if token == "" {
			continue
		}
		attempts = append(attempts, oauthAttempt{Session: session, Token: token})
	}
	sortOAuthAttempts(attempts)
	return attempts, nil
}

func (m *oauthManager) markExhausted(session *oauthSession, reason oauthFailureReason) {
	if session == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.Cooldown > 0 && strings.TrimSpace(session.FilePath) != "" {
		until := time.Now().Add(m.cooldownForReason(reason))
		m.cooldowns[strings.TrimSpace(session.FilePath)] = until
		session.CooldownUntil = until.Format(time.RFC3339)
	}
	session.FailureCount++
	session.LastFailure = string(reason)
	if session.HealthScore == 0 {
		session.HealthScore = 100
	}
	session.HealthScore = maxInt(1, session.HealthScore-healthPenaltyForReason(reason))
	recordProviderRuntimeChange(m.providerName, "oauth", firstNonEmpty(session.Email, session.AccountID, session.FilePath), "oauth_cooldown_"+string(reason), "oauth credential entered cooldown after request failure")
	sessions, err := m.loadAllLocked()
	if err != nil {
		return
	}
	rotated := make([]*oauthSession, 0, len(sessions))
	for _, item := range sessions {
		if item == nil {
			continue
		}
		if item.FilePath == session.FilePath {
			continue
		}
		rotated = append(rotated, item)
	}
	rotated = append(rotated, session)
	m.cached = rotated
}

func (m *oauthManager) disableSession(session *oauthSession, reason oauthFailureReason, detail string) {
	if session == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	session.Disabled = true
	session.DisableReason = string(reason)
	session.FailureCount++
	session.LastFailure = firstNonEmpty(strings.TrimSpace(detail), string(reason))
	session.HealthScore = 0
	session.CooldownUntil = ""
	if path := strings.TrimSpace(session.FilePath); path != "" {
		delete(m.cooldowns, path)
	}
	if err := m.persistSessionLocked(session); err == nil {
		m.cached = appendLoadedSession(m.cached, session)
	}
	recordProviderRuntimeChange(m.providerName, "oauth", firstNonEmpty(session.Email, session.AccountID, session.FilePath), "oauth_disabled_"+string(reason), "oauth credential disabled after unrecoverable upstream error")
}

func (m *oauthManager) markSuccess(session *oauthSession) {
	if session == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	path := strings.TrimSpace(session.FilePath)
	wasCooling := path != "" && session.CooldownUntil != ""
	if path != "" {
		delete(m.cooldowns, path)
		session.CooldownUntil = ""
	}
	if session.HealthScore == 0 {
		session.HealthScore = 100
	} else {
		session.HealthScore = minInt(100, session.HealthScore+3)
	}
	if wasCooling {
		recordProviderRuntimeChange(m.providerName, "oauth", firstNonEmpty(session.Email, session.AccountID, session.FilePath), "oauth_recovered", "oauth cooldown cleared after successful request")
	}
}

func (m *oauthManager) sessionOnCooldown(session *oauthSession) bool {
	if m == nil || session == nil {
		return false
	}
	path := strings.TrimSpace(session.FilePath)
	if path == "" {
		return false
	}
	until, ok := m.cooldowns[path]
	if !ok {
		return false
	}
	if time.Now().Before(until) {
		session.CooldownUntil = until.Format(time.RFC3339)
		return true
	}
	delete(m.cooldowns, path)
	session.CooldownUntil = ""
	return false
}

func (m *oauthManager) cooldownForReason(reason oauthFailureReason) time.Duration {
	base := m.cfg.Cooldown
	switch reason {
	case oauthFailureQuota:
		return base * 4
	case oauthFailureForbidden:
		return base * 2
	case oauthFailureRateLimit:
		return base
	default:
		return base
	}
}

func healthPenaltyForReason(reason oauthFailureReason) int {
	switch reason {
	case oauthFailureQuota:
		return 40
	case oauthFailureForbidden:
		return 25
	case oauthFailureRateLimit:
		return 10
	default:
		return 10
	}
}

func sessionHealthScore(session *oauthSession) int {
	if session == nil {
		return 0
	}
	if session.Disabled {
		return 0
	}
	if session.HealthScore <= 0 {
		return 100
	}
	return session.HealthScore
}

func sortOAuthAttempts(attempts []oauthAttempt) {
	for i := 0; i < len(attempts); i++ {
		for j := i + 1; j < len(attempts); j++ {
			left := sessionHealthScore(attempts[i].Session)
			right := sessionHealthScore(attempts[j].Session)
			if right > left {
				attempts[i], attempts[j] = attempts[j], attempts[i]
			}
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m *oauthManager) credentialFiles() []string {
	return uniqueStrings(append([]string(nil), m.cfg.CredentialFiles...))
}

func (m *oauthManager) startBackgroundRefreshLoop() {
	if m == nil || m.bgCtx == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(m.cfg.RefreshScan)
		defer ticker.Stop()
		for {
			select {
			case <-m.bgCtx.Done():
				return
			case <-ticker.C:
				_, _ = m.refreshExpiringSessions(m.bgCtx, m.cfg.RefreshLead)
			}
		}
	}()
}

func (m *oauthManager) refreshExpiringSessions(ctx context.Context, lead time.Duration) (*ProviderRefreshResult, error) {
	if m == nil {
		return &ProviderRefreshResult{}, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	sessions, err := m.loadAllLocked()
	if err != nil {
		return nil, err
	}
	result := &ProviderRefreshResult{Provider: strings.TrimSpace(m.providerName)}
	for _, session := range sessions {
		if session == nil {
			continue
		}
		result.Checked++
		target := firstNonEmpty(session.Email, session.AccountID, session.FilePath)
		if strings.TrimSpace(session.RefreshToken) == "" {
			result.Skipped++
			result.Accounts = append(result.Accounts, ProviderRefreshAccountResult{Target: target, Status: "skipped", Detail: "missing refresh_token", Expire: session.Expire})
			continue
		}
		if !sessionNeedsRefresh(session, lead) {
			result.Skipped++
			result.Accounts = append(result.Accounts, ProviderRefreshAccountResult{Target: target, Status: "skipped", Detail: "not expiring within lead window", Expire: session.Expire})
			continue
		}
		refreshed, refreshErr := m.refreshSessionLocked(ctx, session)
		if refreshErr != nil {
			result.Failed++
			result.Accounts = append(result.Accounts, ProviderRefreshAccountResult{Target: target, Status: "failed", Detail: refreshErr.Error(), Expire: session.Expire})
			continue
		}
		m.cached = appendLoadedSession(m.cached, refreshed)
		result.Refreshed++
		result.Accounts = append(result.Accounts, ProviderRefreshAccountResult{Target: firstNonEmpty(refreshed.Email, refreshed.AccountID, refreshed.FilePath), Status: "refreshed", Detail: "token refreshed", Expire: refreshed.Expire})
	}
	return result, nil
}

func (m *oauthManager) obtainOAuthCallback(ctx context.Context, state, pkceChallenge string, opts OAuthLoginOptions) (*oauthCallbackResult, error) {
	authURL := m.authorizationURL(state, pkceChallenge)
	if opts.Manual {
		return waitForOAuthCodeManual(authURL, readerOrStdin(opts.Reader))
	}
	return waitForOAuthCode(ctx, m.cfg.CallbackPort, m.cfg.RedirectPath, authURL, opts.NoBrowser)
}

func (m *oauthManager) authorizationURL(state, pkceChallenge string) string {
	v := url.Values{}
	v.Set("client_id", m.cfg.ClientID)
	v.Set("response_type", "code")
	v.Set("redirect_uri", m.cfg.RedirectURL)
	if len(m.cfg.Scopes) > 0 {
		v.Set("scope", strings.Join(m.cfg.Scopes, " "))
	}
	v.Set("state", state)
	if pkceChallenge != "" {
		v.Set("code_challenge", pkceChallenge)
		v.Set("code_challenge_method", "S256")
	}
	switch m.cfg.Provider {
	case defaultCodexOAuthProvider:
		v.Set("prompt", "login")
		v.Set("id_token_add_organizations", "true")
		v.Set("codex_cli_simplified_flow", "true")
	case defaultClaudeOAuthProvider:
		v.Set("code", "true")
	case defaultAntigravityOAuthProvider, defaultGeminiOAuthProvider:
		v.Set("access_type", "offline")
		v.Set("prompt", "consent")
	}
	return m.cfg.AuthURL + "?" + v.Encode()
}

func (m *oauthManager) exchangeCode(ctx context.Context, code, verifier, state string, proxyURL string) (*oauthSession, error) {
	switch m.cfg.Provider {
	case defaultClaudeOAuthProvider:
		reqBody := map[string]any{
			"code":          code,
			"state":         state,
			"grant_type":    "authorization_code",
			"client_id":     m.cfg.ClientID,
			"redirect_uri":  m.cfg.RedirectURL,
			"code_verifier": verifier,
		}
		raw, err := m.doJSONTokenRequest(ctx, reqBody, proxyURL)
		if err != nil {
			return nil, err
		}
		return sessionFromTokenPayload(m.cfg.Provider, raw)
	default:
		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("client_id", m.cfg.ClientID)
		form.Set("code", code)
		form.Set("redirect_uri", m.cfg.RedirectURL)
		if verifier != "" {
			form.Set("code_verifier", verifier)
		}
		if m.cfg.ClientSecret != "" {
			form.Set("client_secret", m.cfg.ClientSecret)
		}
		raw, err := m.doFormTokenRequest(ctx, form, proxyURL)
		if err != nil {
			return nil, err
		}
		session, err := sessionFromTokenPayload(m.cfg.Provider, raw)
		if err != nil {
			return nil, err
		}
		return m.enrichSession(ctx, session)
	}
}

func (m *oauthManager) refreshSessionLocked(ctx context.Context, session *oauthSession) (*oauthSession, error) {
	refreshed, err := m.refreshSessionData(ctx, session)
	if err != nil {
		return nil, err
	}
	refreshed.FilePath = session.FilePath
	refreshed.HealthScore = minInt(100, maxInt(sessionHealthScore(session), refreshed.HealthScore)+5)
	refreshed.FailureCount = session.FailureCount
	refreshed.LastFailure = session.LastFailure
	refreshed.CooldownUntil = session.CooldownUntil
	refreshed.Disabled = session.Disabled
	refreshed.DisableReason = session.DisableReason
	if err := m.persistSessionLocked(refreshed); err != nil {
		return nil, err
	}
	m.cached = appendLoadedSession(m.cached, refreshed)
	recordProviderRuntimeChange(m.providerName, "oauth", firstNonEmpty(refreshed.Email, refreshed.AccountID, refreshed.FilePath), "oauth_refresh_success", "refresh token exchanged for new access token")
	return refreshed, nil
}

func (m *oauthManager) refreshSessionData(ctx context.Context, session *oauthSession) (*oauthSession, error) {
	switch m.cfg.Provider {
	case defaultClaudeOAuthProvider:
		raw, err := m.doJSONTokenRequest(ctx, map[string]any{
			"client_id":     m.cfg.ClientID,
			"grant_type":    "refresh_token",
			"refresh_token": session.RefreshToken,
		}, session.NetworkProxy)
		if err != nil {
			return nil, err
		}
		refreshed, err := sessionFromTokenPayload(m.cfg.Provider, raw)
		if err != nil {
			return nil, err
		}
		return mergeOAuthSession(session, refreshed), nil
	case defaultGeminiOAuthProvider:
		refreshed, err := m.refreshGoogleTokenSession(ctx, session)
		if err != nil {
			return nil, err
		}
		return mergeOAuthSession(session, refreshed), nil
	default:
		form := url.Values{}
		form.Set("client_id", m.cfg.ClientID)
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", session.RefreshToken)
		if m.cfg.ClientSecret != "" {
			form.Set("client_secret", m.cfg.ClientSecret)
		}
		if len(m.cfg.Scopes) > 0 && m.cfg.Provider != defaultQwenOAuthProvider && m.cfg.Provider != defaultKimiOAuthProvider {
			form.Set("scope", strings.Join(m.cfg.Scopes, " "))
		}
		raw, err := m.doFormTokenRequest(ctx, form, session.NetworkProxy)
		if err != nil {
			return nil, err
		}
		refreshed, err := sessionFromTokenPayload(m.cfg.Provider, raw)
		if err != nil {
			return nil, err
		}
		refreshed = mergeOAuthSession(session, refreshed)
		return m.enrichSession(ctx, refreshed)
	}
}

func (m *oauthManager) refreshGoogleTokenSession(ctx context.Context, session *oauthSession) (*oauthSession, error) {
	tokenData := cloneStringAnyMap(session.Token)
	if len(tokenData) == 0 {
		tokenData = map[string]any{}
	}
	tokenURL := firstNonEmpty(asString(tokenData["token_uri"]), m.cfg.TokenURL, defaultGeminiTokenURL)
	clientID := firstNonEmpty(asString(tokenData["client_id"]), m.cfg.ClientID)
	clientSecret := firstNonEmpty(asString(tokenData["client_secret"]), m.cfg.ClientSecret)
	refreshToken := firstNonEmpty(asString(tokenData["refresh_token"]), session.RefreshToken)
	if tokenURL == "" || clientID == "" || refreshToken == "" {
		return nil, fmt.Errorf("oauth token refresh failed: gemini token metadata incomplete")
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	raw, err := m.doFormTokenRequestURL(ctx, tokenURL, form, session.NetworkProxy)
	if err != nil {
		return nil, err
	}
	refreshed, err := sessionFromTokenPayload(m.cfg.Provider, raw)
	if err != nil {
		return nil, err
	}
	tokenData["access_token"] = refreshed.AccessToken
	if refreshed.RefreshToken != "" {
		tokenData["refresh_token"] = refreshed.RefreshToken
	}
	if refreshed.IDToken != "" {
		tokenData["id_token"] = refreshed.IDToken
	}
	if refreshed.Expire != "" {
		tokenData["expiry"] = refreshed.Expire
	}
	tokenData["token_uri"] = tokenURL
	tokenData["client_id"] = clientID
	if clientSecret != "" {
		tokenData["client_secret"] = clientSecret
	}
	if len(m.cfg.Scopes) > 0 && tokenData["scopes"] == nil {
		tokenData["scopes"] = append([]string(nil), m.cfg.Scopes...)
	}
	refreshed.Token = tokenData
	return m.enrichSession(ctx, mergeOAuthSession(session, refreshed))
}

func (m *oauthManager) enrichSession(ctx context.Context, session *oauthSession) (*oauthSession, error) {
	if session == nil {
		return nil, fmt.Errorf("oauth session is nil")
	}
	switch m.cfg.Provider {
	case defaultAntigravityOAuthProvider, defaultGeminiOAuthProvider:
		if strings.TrimSpace(session.Email) == "" && m.cfg.UserInfoURL != "" && session.AccessToken != "" {
			email, err := m.fetchUserEmail(ctx, session.AccessToken, session.NetworkProxy)
			if err == nil {
				session.Email = email
			}
		}
		if m.cfg.Provider == defaultAntigravityOAuthProvider && strings.TrimSpace(session.ProjectID) == "" && session.AccessToken != "" {
			projectID, err := m.fetchAntigravityProjectID(ctx, session.AccessToken, session.NetworkProxy)
			if err == nil {
				session.ProjectID = projectID
			}
		}
	}
	return session, nil
}

func (m *oauthManager) loadAllLocked() ([]*oauthSession, error) {
	if len(m.cached) > 0 {
		return cloneSessions(m.cached), nil
	}
	files := m.credentialFiles()
	out := make([]*oauthSession, 0, len(files))
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			recordProviderRuntimeChange(m.providerName, "oauth", path, "credential_read_failed", err.Error())
			continue
		}
		session, err := parseImportedOAuthSession(m.cfg.Provider, filepath.Base(path), raw)
		if err != nil {
			recordProviderRuntimeChange(m.providerName, "oauth", path, "credential_decode_failed", err.Error())
			continue
		}
		session.FilePath = path
		if until, ok := m.cooldowns[strings.TrimSpace(path)]; ok && time.Now().Before(until) {
			session.CooldownUntil = until.Format(time.RFC3339)
		}
		out = append(out, session)
	}
	m.cached = cloneSessions(out)
	return cloneSessions(out), nil
}

func (m *oauthManager) persistSessionLocked(session *oauthSession) error {
	if session == nil {
		return fmt.Errorf("oauth session is nil")
	}
	m.prepareSessionForPersist(session)
	path := strings.TrimSpace(session.FilePath)
	if path == "" {
		path = m.cfg.CredentialFile
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create oauth credential dir failed: %w", err)
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("encode oauth credential file failed: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write oauth credential file failed: %w", err)
	}
	session.FilePath = path
	return nil
}

func (m *oauthManager) prepareSessionForPersist(session *oauthSession) {
	if session == nil {
		return
	}
	switch m.cfg.Provider {
	case defaultGeminiOAuthProvider:
		if session.Token == nil {
			session.Token = map[string]any{}
		}
		if session.Token["access_token"] == nil && strings.TrimSpace(session.AccessToken) != "" {
			session.Token["access_token"] = strings.TrimSpace(session.AccessToken)
		}
		if session.Token["refresh_token"] == nil && strings.TrimSpace(session.RefreshToken) != "" {
			session.Token["refresh_token"] = strings.TrimSpace(session.RefreshToken)
		}
		if session.Token["id_token"] == nil && strings.TrimSpace(session.IDToken) != "" {
			session.Token["id_token"] = strings.TrimSpace(session.IDToken)
		}
		if session.Token["expiry"] == nil && strings.TrimSpace(session.Expire) != "" {
			session.Token["expiry"] = strings.TrimSpace(session.Expire)
		}
		if session.Token["token_uri"] == nil {
			session.Token["token_uri"] = firstNonEmpty(m.cfg.TokenURL, defaultGeminiTokenURL)
		}
		if session.Token["client_id"] == nil {
			if clientID := strings.TrimSpace(m.cfg.ClientID); clientID != "" {
				session.Token["client_id"] = clientID
			}
		}
		if session.Token["client_secret"] == nil {
			if clientSecret := strings.TrimSpace(m.cfg.ClientSecret); clientSecret != "" {
				session.Token["client_secret"] = clientSecret
			}
		}
		if session.Token["scopes"] == nil {
			session.Token["scopes"] = append([]string(nil), m.cfg.Scopes...)
		}
		if session.Token["universe_domain"] == nil {
			session.Token["universe_domain"] = "googleapis.com"
		}
	case defaultAntigravityOAuthProvider:
		if session.Token == nil {
			session.Token = map[string]any{}
		}
		session.Token["type"] = defaultAntigravityOAuthProvider
		if strings.TrimSpace(session.AccessToken) != "" {
			session.Token["access_token"] = strings.TrimSpace(session.AccessToken)
		}
		if strings.TrimSpace(session.RefreshToken) != "" {
			session.Token["refresh_token"] = strings.TrimSpace(session.RefreshToken)
		}
		if strings.TrimSpace(session.TokenType) != "" {
			session.Token["token_type"] = strings.TrimSpace(session.TokenType)
		}
		if strings.TrimSpace(session.Expire) != "" {
			session.Token["expired"] = strings.TrimSpace(session.Expire)
		}
		if strings.TrimSpace(session.Email) != "" {
			session.Token["email"] = strings.TrimSpace(session.Email)
		}
		if strings.TrimSpace(session.ProjectID) != "" {
			session.Token["project_id"] = strings.TrimSpace(session.ProjectID)
		}
		if strings.TrimSpace(session.Scope) != "" {
			session.Token["scope"] = strings.TrimSpace(session.Scope)
		}
		if strings.TrimSpace(session.Email) != "" {
			session.Token["account_label"] = strings.TrimSpace(session.Email)
		}
	}
}

func (m *oauthManager) applyAccountLabel(session *oauthSession, opts OAuthLoginOptions) error {
	if session == nil {
		return fmt.Errorf("oauth session is nil")
	}
	label := strings.TrimSpace(opts.AccountLabel)
	switch m.cfg.Provider {
	case defaultQwenOAuthProvider:
		if strings.TrimSpace(session.Email) == "" {
			if label == "" {
				return fmt.Errorf("qwen oauth requires account_label when email is unavailable")
			}
			session.Email = label
		}
	default:
		if strings.TrimSpace(session.Email) == "" && label != "" {
			session.Email = label
		}
	}
	return nil
}

func (m *oauthManager) applySessionOptions(session *oauthSession, opts OAuthLoginOptions) error {
	if err := m.applyAccountLabel(session, opts); err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("oauth session is nil")
	}
	proxyURL := firstNonEmpty(opts.NetworkProxy, session.NetworkProxy)
	normalized, err := normalizeOptionalProxyURL(proxyURL)
	if err != nil {
		return err
	}
	session.NetworkProxy = normalized
	return nil
}

func sessionLabel(session *oauthSession) string {
	if session == nil {
		return ""
	}
	return firstNonEmpty(session.Email, session.AccountID, session.ProjectID)
}

func (m *oauthManager) httpClientForSession(session *oauthSession) (*http.Client, error) {
	if session == nil {
		return m.httpClient, nil
	}
	return m.httpClientForProxy(session.NetworkProxy)
}

func (m *oauthManager) httpClientForProxy(proxyURL string) (*http.Client, error) {
	normalized, err := normalizeOptionalProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}
	if normalized == "" {
		return m.httpClient, nil
	}
	m.clientMu.Lock()
	defer m.clientMu.Unlock()
	if client, ok := m.clients[normalized]; ok && client != nil {
		return client, nil
	}
	client, err := newOAuthHTTPClient(m.cfg.Provider, m.timeout, normalized)
	if err != nil {
		return nil, err
	}
	m.clients[normalized] = client
	return client, nil
}

func (m *oauthManager) allocateCredentialPathLocked(session *oauthSession) (string, error) {
	files := m.credentialFiles()
	if len(files) == 1 {
		path := files[0]
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		}
	}
	baseDir := filepath.Dir(m.cfg.CredentialFile)
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return "", err
	}
	label := sanitizeFileToken(firstNonEmpty(session.Email, session.AccountID, session.ProjectID, "account"))
	name := fmt.Sprintf("%s-%s.json", label, time.Now().UTC().Format("20060102-150405"))
	return filepath.Join(baseDir, name), nil
}

func sessionFromTokenPayload(provider string, raw map[string]any) (*oauthSession, error) {
	resp := oauthTokenResponse{}
	payloadBytes, err := json.Marshal(raw)
	if err == nil {
		_ = json.Unmarshal(payloadBytes, &resp)
	}
	session := &oauthSession{
		Provider:     provider,
		Type:         provider,
		AccessToken:  strings.TrimSpace(resp.AccessToken),
		RefreshToken: strings.TrimSpace(resp.RefreshToken),
		IDToken:      strings.TrimSpace(resp.IDToken),
		TokenType:    strings.TrimSpace(resp.TokenType),
		ResourceURL:  strings.TrimSpace(resp.ResourceURL),
		Scope:        strings.TrimSpace(resp.Scope),
		LastRefresh:  time.Now().Format(time.RFC3339),
	}
	if resp.ExpiresIn > 0 {
		session.Expire = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	switch provider {
	case defaultClaudeOAuthProvider:
		session.Email = firstNonEmpty(strings.TrimSpace(resp.Account.EmailAddress), asString(raw["email"]))
		session.AccountID = firstNonEmpty(strings.TrimSpace(resp.Account.UUID), strings.TrimSpace(resp.Organization.UUID))
	case defaultAntigravityOAuthProvider, defaultGeminiOAuthProvider:
		session.Email = asString(raw["email"])
		session.ProjectID = asString(raw["project_id"])
	case defaultQwenOAuthProvider:
		session.Email = asString(raw["email"])
	case defaultKimiOAuthProvider:
		session.DeviceID = asString(raw["device_id"])
	default:
		claims := parseJWTClaims(resp.IDToken)
		session.Email = strings.TrimSpace(fmt.Sprintf("%v", claims["email"]))
		session.AccountID = firstNonEmpty(
			strings.TrimSpace(fmt.Sprintf("%v", claims["https://api.openai.com/auth"])),
			strings.TrimSpace(fmt.Sprintf("%v", claims["account_id"])),
			strings.TrimSpace(fmt.Sprintf("%v", claims["sub"])),
		)
	}
	if session.Email == "" {
		session.Email = firstNonEmpty(asString(raw["email"]), asString(raw["account_id"]))
	}
	if session.AccountID == "" {
		session.AccountID = firstNonEmpty(asString(raw["account_id"]), asString(raw["sub"]))
	}
	if session.Expire == "" {
		session.Expire = firstNonEmpty(asString(raw["expire"]), asString(raw["expired"]), asString(raw["expiry_date"]), asString(raw["expiry"]))
	}
	if provider == defaultGeminiOAuthProvider {
		if tokenMap := mapFromAny(raw["token"]); len(tokenMap) > 0 {
			session.Token = tokenMap
			session.AccessToken = firstNonEmpty(session.AccessToken, asString(tokenMap["access_token"]))
			session.RefreshToken = firstNonEmpty(session.RefreshToken, asString(tokenMap["refresh_token"]))
			session.IDToken = firstNonEmpty(session.IDToken, asString(tokenMap["id_token"]))
			session.Expire = firstNonEmpty(session.Expire, asString(tokenMap["expiry"]), asString(tokenMap["expiry_date"]))
		}
	}
	if session.AccessToken == "" && session.RefreshToken == "" {
		return nil, fmt.Errorf("oauth token payload missing access_token/refresh_token")
	}
	return session, nil
}

func mergeOAuthSession(prev, next *oauthSession) *oauthSession {
	if next == nil {
		return prev
	}
	if prev == nil {
		return next
	}
	merged := *next
	merged.Provider = firstNonEmpty(next.Provider, prev.Provider)
	merged.Type = firstNonEmpty(next.Type, prev.Type)
	merged.RefreshToken = firstNonEmpty(next.RefreshToken, prev.RefreshToken)
	merged.Email = firstNonEmpty(next.Email, prev.Email)
	merged.AccountID = firstNonEmpty(next.AccountID, prev.AccountID)
	merged.ProjectID = firstNonEmpty(next.ProjectID, prev.ProjectID)
	merged.DeviceID = firstNonEmpty(next.DeviceID, prev.DeviceID)
	merged.ResourceURL = firstNonEmpty(next.ResourceURL, prev.ResourceURL)
	merged.NetworkProxy = firstNonEmpty(next.NetworkProxy, prev.NetworkProxy)
	merged.Scope = firstNonEmpty(next.Scope, prev.Scope)
	merged.Models = append([]string(nil), prev.Models...)
	if len(next.Models) > 0 {
		merged.Models = append([]string(nil), next.Models...)
	}
	merged.FilePath = prev.FilePath
	if merged.Expire == "" {
		merged.Expire = prev.Expire
	}
	if len(next.Token) > 0 {
		merged.Token = cloneStringAnyMap(next.Token)
	} else if len(prev.Token) > 0 {
		merged.Token = cloneStringAnyMap(prev.Token)
	}
	if merged.LastRefresh == "" {
		merged.LastRefresh = time.Now().Format(time.RFC3339)
	}
	return &merged
}

func sessionNeedsRefresh(session *oauthSession, lead time.Duration) bool {
	if session == nil {
		return false
	}
	expireAt, err := time.Parse(time.RFC3339, strings.TrimSpace(session.Expire))
	if err != nil {
		return false
	}
	return time.Now().Add(lead).After(expireAt)
}

func fetchOpenAIModels(ctx context.Context, client *http.Client, apiBase, token string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointFor(apiBase, "/models"), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read models response failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch models failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode models response failed: %w", err)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func waitForOAuthCode(ctx context.Context, port int, callbackPath, authURL string, noBrowser bool) (*oauthCallbackResult, error) {
	if port <= 0 {
		return nil, fmt.Errorf("oauth callback port must be > 0")
	}
	if callbackPath == "" {
		callbackPath = "/auth/callback"
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("oauth callback listener failed: %w", err)
	}
	defer listener.Close()

	resultCh := make(chan *oauthCallbackResult, 1)
	errCh := make(chan error, 1)
	server := &http.Server{Handler: http.NewServeMux()}
	server.Handler.(*http.ServeMux).HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if errText := strings.TrimSpace(query.Get("error")); errText != "" {
			http.Error(w, "OAuth login failed: "+errText, http.StatusBadRequest)
			select {
			case resultCh <- &oauthCallbackResult{Err: errText, State: strings.TrimSpace(query.Get("state"))}:
			default:
			}
			return
		}
		code := strings.TrimSpace(query.Get("code"))
		if code == "" {
			http.Error(w, "missing oauth code", http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("oauth callback missing code"):
			default:
			}
			return
		}
		_, _ = io.WriteString(w, "OAuth login complete. You can close this window and return to clawgo.")
		select {
		case resultCh <- &oauthCallbackResult{Code: code, State: strings.TrimSpace(query.Get("state"))}:
		default:
		}
	})
	go func() { _ = server.Serve(listener) }()

	fmt.Printf("Open this URL to continue OAuth login:\n%s\n", authURL)
	if !noBrowser {
		if err := openBrowser(authURL); err != nil {
			fmt.Printf("Automatic browser open failed: %v\n", err)
		}
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case result := <-resultCh:
		return result, nil
	}
}

func waitForOAuthCodeManual(authURL string, reader io.Reader) (*oauthCallbackResult, error) {
	fmt.Printf("Open this URL to continue OAuth login:\n%s\n", authURL)
	fmt.Println("After login, copy the final callback URL from the browser and paste it below.")
	br := bufio.NewReader(reader)
	fmt.Print("callback_url: ")
	line, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("read callback url failed: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, fmt.Errorf("callback url is empty")
	}
	return parseOAuthCallbackURL(line)
}

func readerOrStdin(reader io.Reader) io.Reader {
	if reader != nil {
		return reader
	}
	return os.Stdin
}

func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

func generatePKCE() (string, string, error) {
	verifier, err := randomURLToken(64)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func randomURLToken(size int) (string, error) {
	if size <= 0 {
		size = 32
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func parseJWTClaims(token string) map[string]any {
	token = strings.TrimSpace(token)
	if token == "" {
		return map[string]any{}
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return map[string]any{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return map[string]any{}
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		return map[string]any{}
	}
	return claims
}

func extractOAuthBalanceMetadata(session *oauthSession) (planType, quotaSource, balanceLabel, balanceDetail, subActiveStart, subActiveUntil string) {
	if session == nil {
		return "", "", "", "", "", ""
	}
	provider := normalizeOAuthProvider(session.Provider)
	switch provider {
	case defaultCodexOAuthProvider:
		claims := parseJWTClaims(session.IDToken)
		auth := mapFromAny(claims["https://api.openai.com/auth"])
		planType = firstNonEmpty(asString(auth["chatgpt_plan_type"]), asString(auth["plan_type"]))
		subActiveStart = normalizeBalanceTime(firstNonEmpty(asString(auth["chatgpt_subscription_active_start"]), asString(auth["subscription_active_start"])))
		subActiveUntil = normalizeBalanceTime(firstNonEmpty(asString(auth["chatgpt_subscription_active_until"]), asString(auth["subscription_active_until"])))
		if planType != "" || subActiveUntil != "" || subActiveStart != "" {
			quotaSource = provider
			if planType != "" {
				balanceLabel = strings.ToUpper(planType)
			} else {
				balanceLabel = "subscription"
			}
			switch {
			case subActiveStart != "" && subActiveUntil != "":
				balanceDetail = formatSubscriptionWindow(subActiveStart, subActiveUntil)
			case subActiveUntil != "":
				balanceDetail = fmt.Sprintf("until %s", subActiveUntil)
			case subActiveStart != "":
				balanceDetail = fmt.Sprintf("from %s", subActiveStart)
			}
		}
	case defaultGeminiOAuthProvider, defaultAntigravityOAuthProvider:
		if pid := strings.TrimSpace(session.ProjectID); pid != "" {
			quotaSource = provider
			balanceLabel = "project"
			balanceDetail = pid
		}
	}
	return planType, quotaSource, balanceLabel, balanceDetail, subActiveStart, subActiveUntil
}

func normalizeBalanceTime(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.Format(time.RFC3339)
		}
	}
	return trimmed
}

func formatSubscriptionWindow(startRaw, untilRaw string) string {
	startRaw = strings.TrimSpace(startRaw)
	untilRaw = strings.TrimSpace(untilRaw)
	if startRaw == "" || untilRaw == "" {
		return strings.TrimSpace(strings.TrimSpace(startRaw) + " ~ " + strings.TrimSpace(untilRaw))
	}
	startAt, okStart := parseBalanceTime(startRaw)
	untilAt, okUntil := parseBalanceTime(untilRaw)
	if !okStart || !okUntil || !untilAt.After(startAt) {
		return fmt.Sprintf("%s ~ %s", startRaw, untilRaw)
	}
	now := time.Now().UTC()
	total := untilAt.Sub(startAt)
	elapsed := now.Sub(startAt)
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed > total {
		elapsed = total
	}
	usedPct := int(math.Round(float64(elapsed) * 100 / float64(total)))
	if usedPct < 0 {
		usedPct = 0
	}
	if usedPct > 100 {
		usedPct = 100
	}
	return fmt.Sprintf("%s ~ %s (%d%% used, %d%% left)", startRaw, untilRaw, usedPct, 100-usedPct)
}

func parseBalanceTime(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func trimNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func cloneSessions(values []*oauthSession) []*oauthSession {
	out := make([]*oauthSession, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		cp := *value
		cp.Models = append([]string(nil), value.Models...)
		cp.Token = cloneStringAnyMap(value.Token)
		out = append(out, &cp)
	}
	return out
}

func appendLoadedSession(values []*oauthSession, session *oauthSession) []*oauthSession {
	filtered := make([]*oauthSession, 0, len(values)+1)
	for _, value := range values {
		if value == nil || value.FilePath == session.FilePath {
			continue
		}
		filtered = append(filtered, value)
	}
	filtered = append(filtered, session)
	return filtered
}

func sanitizeFileToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "account"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "account"
	}
	return out
}

func durationFromSeconds(value int, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return time.Duration(value) * time.Second
}

func parseOAuthCallbackURL(raw string) (*oauthCallbackResult, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid callback url: %w", err)
	}
	query := parsed.Query()
	if errText := strings.TrimSpace(query.Get("error")); errText != "" {
		return nil, fmt.Errorf("oauth callback returned error: %s", errText)
	}
	code := strings.TrimSpace(query.Get("code"))
	if code == "" {
		return nil, fmt.Errorf("oauth callback missing code")
	}
	return &oauthCallbackResult{
		Code:  code,
		State: strings.TrimSpace(query.Get("state")),
	}, nil
}

func parseImportedOAuthSession(provider, fileName string, data []byte) (*oauthSession, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	session := &oauthSession{
		Provider: provider,
		Type:     firstNonEmpty(asString(raw["type"]), provider),
	}
	if tokenMap := mapFromAny(raw["token"]); len(tokenMap) > 0 {
		session.Token = tokenMap
		session.AccessToken = firstNonEmpty(session.AccessToken, asString(tokenMap["access_token"]))
		session.RefreshToken = firstNonEmpty(session.RefreshToken, asString(tokenMap["refresh_token"]))
		session.IDToken = firstNonEmpty(session.IDToken, asString(tokenMap["id_token"]))
		session.TokenType = firstNonEmpty(session.TokenType, asString(tokenMap["token_type"]))
		session.Expire = firstNonEmpty(session.Expire, asString(tokenMap["expiry"]), asString(tokenMap["expiry_date"]))
		if session.Token == nil {
			session.Token = map[string]any{}
		}
	}
	if authMap := mapFromAny(raw["auth"]); len(authMap) > 0 {
		session.AccessToken = firstNonEmpty(session.AccessToken, asString(authMap["access_token"]))
		session.RefreshToken = firstNonEmpty(session.RefreshToken, asString(authMap["refresh_token"]))
		session.IDToken = firstNonEmpty(session.IDToken, asString(authMap["id_token"]))
		session.TokenType = firstNonEmpty(session.TokenType, asString(authMap["token_type"]))
		session.Expire = firstNonEmpty(session.Expire, asString(authMap["expiry"]), asString(authMap["expired"]))
	}
	session.AccessToken = firstNonEmpty(session.AccessToken, asString(raw["access_token"]))
	session.RefreshToken = firstNonEmpty(session.RefreshToken, asString(raw["refresh_token"]))
	session.IDToken = firstNonEmpty(session.IDToken, asString(raw["id_token"]))
	session.TokenType = firstNonEmpty(session.TokenType, asString(raw["token_type"]))
	session.AccountID = firstNonEmpty(asString(raw["account_id"]), asString(raw["sub"]))
	session.Email = firstNonEmpty(
		asString(raw["email"]),
		asString(raw["account_email"]),
		asString(raw["alias"]),
		asString(raw["account_label"]),
		asString(raw["label"]),
	)
	session.Expire = firstNonEmpty(session.Expire, asString(raw["expire"]), asString(raw["expired"]), asString(raw["expiry_date"]), asString(raw["expiry"]))
	session.LastRefresh = firstNonEmpty(asString(raw["last_refresh"]), time.Now().Format(time.RFC3339))
	session.ProjectID = firstNonEmpty(asString(raw["project_id"]), asString(raw["projectId"]))
	session.DeviceID = firstNonEmpty(asString(raw["device_id"]), asString(raw["deviceId"]))
	session.ResourceURL = asString(raw["resource_url"])
	session.NetworkProxy = firstNonEmpty(asString(raw["network_proxy"]), asString(raw["proxy_url"]), asString(raw["http_proxy"]))
	session.Scope = firstNonEmpty(asString(raw["scope"]), asString(raw["scopes"]))
	if models := stringSliceFromAny(raw["models"]); len(models) > 0 {
		session.Models = models
	}
	if session.Token != nil {
		session.Email = firstNonEmpty(
			session.Email,
			asString(session.Token["email"]),
			asString(session.Token["alias"]),
			asString(session.Token["account_label"]),
			asString(session.Token["label"]),
		)
		session.ProjectID = firstNonEmpty(session.ProjectID, asString(session.Token["project_id"]), asString(session.Token["projectId"]))
		session.DeviceID = firstNonEmpty(session.DeviceID, asString(session.Token["device_id"]), asString(session.Token["deviceId"]))
		session.NetworkProxy = firstNonEmpty(session.NetworkProxy, asString(session.Token["network_proxy"]), asString(session.Token["proxy_url"]), asString(session.Token["http_proxy"]))
		session.Scope = firstNonEmpty(session.Scope, asString(session.Token["scope"]), asString(session.Token["scopes"]))
	}
	if claims := parseJWTClaims(session.IDToken); len(claims) > 0 {
		session.Email = firstNonEmpty(session.Email, strings.TrimSpace(fmt.Sprintf("%v", claims["email"])))
		session.AccountID = firstNonEmpty(
			session.AccountID,
			strings.TrimSpace(fmt.Sprintf("%v", claims["https://api.openai.com/auth"])),
			strings.TrimSpace(fmt.Sprintf("%v", claims["account_id"])),
			strings.TrimSpace(fmt.Sprintf("%v", claims["sub"])),
		)
	}
	switch provider {
	case defaultClaudeOAuthProvider:
		if session.Expire == "" {
			session.Expire = firstNonEmpty(asString(raw["expired"]), asString(raw["expire"]))
		}
	case defaultGeminiOAuthProvider:
		if session.Token == nil {
			if tokenMap := mapFromAny(raw["token"]); len(tokenMap) > 0 {
				session.Token = tokenMap
			}
		}
		if session.Token == nil {
			session.Token = map[string]any{}
		}
		if session.Token["token_uri"] == nil {
			session.Token["token_uri"] = defaultGeminiTokenURL
		}
		if session.Token["client_id"] == nil {
			if clientID := strings.TrimSpace(defaultGeminiClientID); clientID != "" {
				session.Token["client_id"] = clientID
			}
		}
		if session.Token["client_secret"] == nil {
			if clientSecret := strings.TrimSpace(defaultGeminiClientSecret); clientSecret != "" {
				session.Token["client_secret"] = clientSecret
			}
		}
		if session.Token["scopes"] == nil {
			session.Token["scopes"] = append([]string(nil), defaultGoogleScopes...)
		}
	case defaultQwenOAuthProvider:
		if session.Expire == "" {
			session.Expire = firstNonEmpty(asString(raw["expired"]), asString(raw["expire"]))
		}
	case defaultKimiOAuthProvider:
		if session.Expire == "" {
			session.Expire = firstNonEmpty(asString(raw["expired"]), asString(raw["expire"]))
		}
	}
	if session.AccessToken == "" && session.RefreshToken == "" {
		return nil, fmt.Errorf("auth.json missing access_token/refresh_token in %s", fileName)
	}
	return session, nil
}

func asString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func mapFromAny(value any) map[string]any {
	raw, ok := value.(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		out[k] = v
	}
	return out
}

func stringSliceFromAny(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			return append([]string(nil), typed...)
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := asString(item); s != "" {
			out = append(out, s)
		}
	}
	return uniqueStrings(out)
}

func cloneStringAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func defaultInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func (m *oauthManager) doFormTokenRequest(ctx context.Context, form url.Values, proxyURL string) (map[string]any, error) {
	return m.doFormTokenRequestURL(ctx, m.cfg.TokenURL, form, proxyURL)
}

func (m *oauthManager) doFormTokenRequestURL(ctx context.Context, endpoint string, form url.Values, proxyURL string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return m.doJSONRequest(req, "oauth token request", proxyURL)
}

func (m *oauthManager) doJSONTokenRequest(ctx context.Context, payload map[string]any, proxyURL string) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.TokenURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return m.doJSONRequest(req, "oauth token request", proxyURL)
}

func (m *oauthManager) doJSONRequest(req *http.Request, label, proxyURL string) (map[string]any, error) {
	client, err := m.httpClientForProxy(proxyURL)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w", label, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s read failed: %w", label, err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s failed: status=%d body=%s", label, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("%s decode failed: %w", label, err)
	}
	return raw, nil
}

func (m *oauthManager) fetchUserEmail(ctx context.Context, token, proxyURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.cfg.UserInfoURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	raw, err := m.doJSONRequest(req, "oauth userinfo request", proxyURL)
	if err != nil {
		return "", err
	}
	email := asString(raw["email"])
	if email == "" {
		return "", fmt.Errorf("oauth userinfo missing email")
	}
	return email, nil
}

func (m *oauthManager) fetchAntigravityProjectID(ctx context.Context, token, proxyURL string) (string, error) {
	endpointURL := fmt.Sprintf("%s/%s:loadCodeAssist", defaultAntigravityAPIEndpoint, defaultAntigravityAPIVersion)
	body := `{"metadata":{"ideType":"ANTIGRAVITY","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", defaultAntigravityAPIUserAgent)
	req.Header.Set("X-Goog-Api-Client", defaultAntigravityAPIClient)
	req.Header.Set("Client-Metadata", defaultAntigravityClientMeta)
	client, err := m.httpClientForProxy(proxyURL)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("antigravity project lookup failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", err
	}
	projectID := strings.TrimSpace(asString(payload["cloudaicompanionProject"]))
	if projectID == "" {
		if projectMap := mapFromAny(payload["cloudaicompanionProject"]); len(projectMap) > 0 {
			projectID = strings.TrimSpace(asString(projectMap["id"]))
		}
	}
	if projectID == "" {
		return "", fmt.Errorf("antigravity project lookup missing project id")
	}
	return projectID, nil
}

func (m *oauthManager) startDeviceFlow(ctx context.Context, opts OAuthLoginOptions) (*OAuthPendingFlow, error) {
	if m.cfg.FlowKind != oauthFlowDevice {
		return nil, fmt.Errorf("oauth provider %s does not use device flow", m.cfg.Provider)
	}
	form := url.Values{}
	form.Set("client_id", m.cfg.ClientID)
	switch m.cfg.Provider {
	case defaultQwenOAuthProvider:
		verifier, challenge, err := generatePKCE()
		if err != nil {
			return nil, err
		}
		if len(m.cfg.Scopes) > 0 {
			form.Set("scope", strings.Join(m.cfg.Scopes, " "))
		}
		form.Set("code_challenge", challenge)
		form.Set("code_challenge_method", "S256")
		raw, err := m.doFormDeviceRequest(ctx, m.cfg.DeviceCodeURL, form, opts.NetworkProxy)
		if err != nil {
			return nil, err
		}
		device, err := parseDeviceFlowPayload(raw)
		if err != nil {
			return nil, err
		}
		return &OAuthPendingFlow{
			Mode:         oauthFlowDevice,
			AuthURL:      firstNonEmpty(device.VerificationURIComplete, device.VerificationURI),
			UserCode:     device.UserCode,
			DeviceCode:   device.DeviceCode,
			PKCEVerifier: verifier,
			IntervalSec:  defaultInt(device.Interval, 5),
			ExpiresAt:    deviceExpiry(device.ExpiresIn),
			Instructions: "Open the verification URL, finish authorization, then click continue to let the gateway poll for tokens.",
		}, nil
	case defaultKimiOAuthProvider:
		raw, err := m.doFormDeviceRequest(ctx, m.cfg.DeviceCodeURL, form, opts.NetworkProxy)
		if err != nil {
			return nil, err
		}
		device, err := parseDeviceFlowPayload(raw)
		if err != nil {
			return nil, err
		}
		return &OAuthPendingFlow{
			Mode:         oauthFlowDevice,
			AuthURL:      firstNonEmpty(device.VerificationURIComplete, device.VerificationURI),
			UserCode:     device.UserCode,
			DeviceCode:   device.DeviceCode,
			IntervalSec:  defaultInt(device.Interval, 5),
			ExpiresAt:    deviceExpiry(device.ExpiresIn),
			Instructions: "Open the verification URL, finish authorization, then click continue to let the gateway poll for tokens.",
		}, nil
	default:
		return nil, fmt.Errorf("oauth device flow not implemented for provider %s", m.cfg.Provider)
	}
}

func (m *oauthManager) doFormDeviceRequest(ctx context.Context, endpoint string, form url.Values, proxyURL string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if m.cfg.Provider == defaultKimiOAuthProvider {
		req.Header.Set("X-Msh-Platform", "clawgo")
		req.Header.Set("X-Msh-Version", "1.0.0")
		req.Header.Set("X-Msh-Device-Name", "clawgo")
		req.Header.Set("X-Msh-Device-Model", runtime.GOOS+" "+runtime.GOARCH)
		req.Header.Set("X-Msh-Device-Id", randomDeviceID())
	}
	return m.doJSONRequest(req, "oauth device request", proxyURL)
}

func parseDeviceFlowPayload(raw map[string]any) (*oauthDeviceCodeResponse, error) {
	body, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var device oauthDeviceCodeResponse
	if err := json.Unmarshal(body, &device); err != nil {
		return nil, err
	}
	if strings.TrimSpace(device.DeviceCode) == "" {
		return nil, fmt.Errorf("oauth device flow missing device_code")
	}
	return &device, nil
}

func deviceExpiry(expiresIn int) string {
	if expiresIn <= 0 {
		return ""
	}
	return time.Now().Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339)
}

func randomDeviceID() string {
	token, err := randomURLToken(24)
	if err != nil {
		return "clawgo-device"
	}
	return token
}

func (m *oauthManager) completeDeviceFlow(ctx context.Context, apiBase string, flow *OAuthPendingFlow, opts OAuthLoginOptions) (*oauthSession, []string, error) {
	session, err := m.pollDeviceToken(ctx, flow, opts.NetworkProxy)
	if err != nil {
		return nil, nil, err
	}
	if err := m.applySessionOptions(session, opts); err != nil {
		return nil, nil, err
	}
	client, err := m.httpClientForSession(session)
	if err != nil {
		return nil, nil, err
	}
	models, _ := fetchOpenAIModels(ctx, client, apiBase, session.AccessToken)
	if len(models) > 0 {
		session.Models = append([]string(nil), models...)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	path, err := m.allocateCredentialPathLocked(session)
	if err != nil {
		return nil, nil, err
	}
	session.FilePath = path
	if err := m.persistSessionLocked(session); err != nil {
		return nil, nil, err
	}
	m.cached = appendLoadedSession(m.cached, session)
	m.cfg.CredentialFiles = uniqueStrings(append(m.cfg.CredentialFiles, path))
	m.cfg.CredentialFile = m.cfg.CredentialFiles[0]
	return session, models, nil
}

func (m *oauthManager) pollDeviceToken(ctx context.Context, flow *OAuthPendingFlow, proxyURL string) (*oauthSession, error) {
	if flow == nil || strings.TrimSpace(flow.DeviceCode) == "" {
		return nil, fmt.Errorf("oauth device flow missing device code")
	}
	interval := time.Duration(defaultInt(flow.IntervalSec, 5)) * time.Second
	deadline := time.Now().Add(m.cfg.DevicePollMax)
	if expireAt, err := time.Parse(time.RFC3339, strings.TrimSpace(flow.ExpiresAt)); err == nil && expireAt.Before(deadline) {
		deadline = expireAt
	}
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("oauth device flow timed out")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		form := url.Values{}
		form.Set("grant_type", m.cfg.DeviceGrantType)
		form.Set("client_id", m.cfg.ClientID)
		form.Set("device_code", flow.DeviceCode)
		if flow.PKCEVerifier != "" {
			form.Set("code_verifier", flow.PKCEVerifier)
		}
		raw, err := m.doFormTokenRequest(ctx, form, proxyURL)
		if err == nil {
			session, convErr := sessionFromTokenPayload(m.cfg.Provider, raw)
			if convErr != nil {
				return nil, convErr
			}
			return m.enrichSession(ctx, session)
		}
		errText := strings.ToLower(err.Error())
		switch {
		case strings.Contains(errText, "authorization_pending"):
			continue
		case strings.Contains(errText, "slow_down"):
			if interval < 10*time.Second {
				interval = interval + time.Second
			}
			continue
		case strings.Contains(errText, "access_denied"):
			return nil, fmt.Errorf("oauth device flow denied by user")
		case strings.Contains(errText, "expired_token"):
			return nil, fmt.Errorf("oauth device code expired")
		default:
			return nil, err
		}
	}
}
