package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/YspCoder/clawgo/pkg/nodes"
	"github.com/gorilla/websocket"
)

func TestCheckAuthAllowsBearerAndCookieOnly(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "secret-token", nil)

	bearerReq := httptest.NewRequest(http.MethodGet, "/", nil)
	bearerReq.Header.Set("Authorization", "Bearer secret-token")
	if !srv.checkAuth(bearerReq) {
		t.Fatalf("expected bearer auth to succeed")
	}

	cookieReq := httptest.NewRequest(http.MethodGet, "/", nil)
	cookieReq.AddCookie(&http.Cookie{Name: "clawgo_webui_token", Value: "secret-token"})
	if !srv.checkAuth(cookieReq) {
		t.Fatalf("expected cookie auth to succeed")
	}

	queryReq := httptest.NewRequest(http.MethodGet, "/?token=secret-token", nil)
	if srv.checkAuth(queryReq) {
		t.Fatalf("expected query token auth to fail")
	}

	refererReq := httptest.NewRequest(http.MethodGet, "/", nil)
	refererReq.Header.Set("Referer", "https://example.com/?token=secret-token")
	if srv.checkAuth(refererReq) {
		t.Fatalf("expected referer token auth to fail")
	}
}

func TestWithCORSRejectsInvalidOrigin(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nil)
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/config", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "javascript:alert(1)")
	rec := httptest.NewRecorder()

	srv.withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestWithCORSAcceptsSameOrigin(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nil)
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/config", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()

	srv.withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Fatalf("unexpected allow-origin header %q", got)
	}
}

func TestWithCORSAcceptsCrossOrigin(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nil)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/config", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "https://web.example")
	rec := httptest.NewRecorder()

	srv.withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://web.example" {
		t.Fatalf("unexpected allow-origin header %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("expected allow credentials, got %q", got)
	}
}

func TestHandleNodeConnectRejectsInvalidOrigin(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	mux := http.NewServeMux()
	mux.HandleFunc("/nodes/connect", srv.handleNodeConnect)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/nodes/connect"
	dialer := websocket.Dialer{}
	headers := http.Header{"Origin": []string{"javascript:alert(1)"}}
	conn, resp, err := dialer.Dial(wsURL, headers)
	if err == nil {
		conn.Close()
		t.Fatalf("expected websocket handshake to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 response, got %#v", resp)
	}
}

func TestHandleNodeConnectAcceptsCrossOrigin(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	mux := http.NewServeMux()
	mux.HandleFunc("/nodes/connect", srv.handleNodeConnect)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/nodes/connect"
	dialer := websocket.Dialer{}
	headers := http.Header{"Origin": []string{"https://web.example"}}
	conn, resp, err := dialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("expected websocket handshake to succeed, resp=%#v err=%v", resp, err)
	}
	_ = conn.Close()
}

func TestHandleWebUISetsCookieForBearerOnly(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "secret-token", nil)

	bearerReq := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	bearerReq.Header.Set("Authorization", "Bearer secret-token")
	bearerRec := httptest.NewRecorder()
	srv.handleWebUI(bearerRec, bearerReq)
	if len(bearerRec.Result().Cookies()) == 0 {
		t.Fatalf("expected bearer-authenticated UI request to set cookie")
	}

	cookieReq := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	cookieReq.AddCookie(&http.Cookie{Name: "clawgo_webui_token", Value: "secret-token"})
	cookieRec := httptest.NewRecorder()
	srv.handleWebUI(cookieRec, cookieReq)
	if len(cookieRec.Result().Cookies()) != 0 {
		t.Fatalf("expected cookie-authenticated UI request not to reset cookie")
	}
}

func TestHandleWebUIAuthSessionSetsCrossSiteCookieForAllowedOrigin(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "secret-token", nil)

	req := httptest.NewRequest(http.MethodPost, "http://gateway.example/api/auth/session", nil)
	req.Host = "gateway.example"
	req.Header.Set("Origin", "https://web.example")
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()

	srv.withCORS(http.HandlerFunc(srv.handleWebUIAuthSession)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}
	if cookies[0].SameSite != http.SameSiteNoneMode {
		t.Fatalf("expected SameSite=None for cross-site session, got %v", cookies[0].SameSite)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://web.example" {
		t.Fatalf("unexpected allow-origin header %q", got)
	}
}

func TestHandleWebUIUploadDoesNotExposeAbsolutePath(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nil)
	var form bytes.Buffer
	mw := multipartWriter(t, &form, "file", "demo.txt", []byte("upload-body"))
	req := httptest.NewRequest(http.MethodPost, "/api/upload", &form)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.handleWebUIUpload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := payload["path"]; ok {
		t.Fatalf("expected upload response to omit absolute path: %+v", payload)
	}
	if strings.TrimSpace(payload["media"].(string)) == "" {
		t.Fatalf("expected media handle in response: %+v", payload)
	}
}

func multipartWriter(t *testing.T, dst *bytes.Buffer, fieldName, filename string, body []byte) *multipart.Writer {
	t.Helper()
	mw := multipart.NewWriter(dst)
	part, err := mw.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return mw
}

func TestHandleWebUIProviderOAuthStartRejectsGet(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/provider/oauth/start?provider=openai", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUIProviderOAuthStart(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}
