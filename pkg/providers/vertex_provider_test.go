package providers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/config"
)

func TestCreateProviderByNameRoutesVertexProvider(t *testing.T) {
	cfg := &config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"vertex": {
					TimeoutSec: 30,
					Models:     []string{"gemini-2.5-pro"},
				},
			},
		},
	}
	provider, err := CreateProviderByName(cfg, "vertex")
	if err != nil {
		t.Fatalf("CreateProviderByName error: %v", err)
	}
	if _, ok := provider.(*VertexProvider); !ok {
		t.Fatalf("provider = %T, want *VertexProvider", provider)
	}
}

func TestVertexEndpointWithProjectLocation(t *testing.T) {
	p := NewVertexProvider("vertex", "", "", "gemini-2.5-pro", false, "oauth", 5*time.Second, nil)
	endpoint := p.endpoint(authAttempt{
		kind: "oauth",
		session: &oauthSession{
			ProjectID: "demo-project",
			Token: map[string]any{
				"location": "asia-east1",
			},
		},
	}, "gemini-2.5-pro", "generateContent", false, nil)
	want := "https://asia-east1-aiplatform.googleapis.com/v1/projects/demo-project/locations/asia-east1/publishers/google/models/gemini-2.5-pro:generateContent"
	if endpoint != want {
		t.Fatalf("endpoint = %q, want %q", endpoint, want)
	}
}

func TestVertexEndpointUsesPredictForImagen(t *testing.T) {
	p := NewVertexProvider("vertex", "", "", "imagen-4.0-generate-001", false, "oauth", 5*time.Second, nil)
	endpoint := p.endpoint(authAttempt{
		kind: "oauth",
		session: &oauthSession{
			ProjectID: "demo-project",
			Token: map[string]any{
				"location": "us-central1",
			},
		},
	}, "imagen-4.0-generate-001", "generateContent", false, nil)
	want := "https://us-central1-aiplatform.googleapis.com/v1/projects/demo-project/locations/us-central1/publishers/google/models/imagen-4.0-generate-001:predict"
	if endpoint != want {
		t.Fatalf("endpoint = %q, want %q", endpoint, want)
	}
}

func TestVertexEndpointUsesGlobalBaseForGlobalLocation(t *testing.T) {
	p := NewVertexProvider("vertex", "", "", "gemini-2.5-pro", false, "oauth", 5*time.Second, nil)
	endpoint := p.endpoint(authAttempt{
		kind: "oauth",
		session: &oauthSession{
			ProjectID: "demo-project",
			Token: map[string]any{
				"location": "global",
			},
		},
	}, "gemini-2.5-pro", "generateContent", false, nil)
	want := "https://aiplatform.googleapis.com/v1/projects/demo-project/locations/global/publishers/google/models/gemini-2.5-pro:generateContent"
	if endpoint != want {
		t.Fatalf("endpoint = %q, want %q", endpoint, want)
	}
}

func TestConvertVertexImagenRequest(t *testing.T) {
	payload := map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]any{{"text": "draw a cat"}}},
		},
		"generationConfig": map[string]any{
			"aspectRatio":    "1:1",
			"sampleCount":    2.0,
			"negativePrompt": "blurry",
		},
	}
	req, err := convertVertexImagenRequest(payload)
	if err != nil {
		t.Fatalf("convertVertexImagenRequest error: %v", err)
	}
	instances, _ := req["instances"].([]map[string]any)
	if len(instances) != 1 || instances[0]["prompt"] != "draw a cat" {
		t.Fatalf("unexpected instances: %+v", instances)
	}
	params, _ := req["parameters"].(map[string]any)
	if params["aspectRatio"] != "1:1" || params["sampleCount"] != 2 {
		t.Fatalf("unexpected parameters: %+v", params)
	}
}

func TestConvertVertexImagenToGeminiResponse(t *testing.T) {
	body := []byte(`{"predictions":[{"bytesBase64Encoded":"abcd","mimeType":"image/png"}]}`)
	converted := convertVertexImagenToGeminiResponse(body, "imagen-4.0-generate-001")
	resp, err := parseGeminiResponse(converted)
	if err != nil {
		t.Fatalf("parseGeminiResponse error: %v", err)
	}
	if resp.FinishReason != "STOP" {
		t.Fatalf("finish reason = %q", resp.FinishReason)
	}
}

func TestVertexProjectLocationFallsBackToProjectAlias(t *testing.T) {
	projectID, location, ok := vertexProjectLocation(authAttempt{
		kind: "oauth",
		session: &oauthSession{
			Token: map[string]any{
				"project":  "demo-project",
				"location": "asia-east1",
			},
		},
	}, nil)
	if !ok || projectID != "demo-project" || location != "asia-east1" {
		t.Fatalf("unexpected project/location: ok=%v project=%q location=%q", ok, projectID, location)
	}
}

func TestVertexServiceAccountJSONBackfillsProjectID(t *testing.T) {
	session := &oauthSession{
		ProjectID: "demo-project",
		Token: map[string]any{
			"service_account": map[string]any{
				"type":         "service_account",
				"client_email": "svc@example.com",
				"private_key":  "key",
				"token_uri":    "https://example.com/token",
			},
		},
	}
	data, err := vertexServiceAccountJSON(session)
	if err != nil {
		t.Fatalf("vertexServiceAccountJSON error: %v", err)
	}
	if !strings.Contains(string(data), `"project_id":"demo-project"`) {
		t.Fatalf("expected project_id in service account json, got %s", string(data))
	}
}

func TestVertexProviderChatWithAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/publishers/google/models/gemini-2.5-pro:generateContent" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("x-goog-api-key"); got != "token" {
			t.Fatalf("x-goog-api-key = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`))
	}))
	defer server.Close()

	p := NewVertexProvider("vertex", "token", server.URL, "gemini-2.5-pro", false, "api_key", 5*time.Second, nil)
	resp, err := p.Chat(t.Context(), []Message{{Role: "user", Content: "hello"}}, nil, "gemini-2.5-pro", nil)
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
}

func TestVertexProviderChatWithServiceAccount(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"sa-token","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/demo-project/locations/us-central1/publishers/google/models/gemini-2.5-pro:generateContent" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sa-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`))
	}))
	defer apiServer.Close()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	p := NewVertexProvider("vertex", "", apiServer.URL, "gemini-2.5-pro", false, "oauth", 5*time.Second, nil)
	p.base.oauth = &oauthManager{}
	p.base.authMode = "oauth"
	p.base.oauth = &oauthManager{
		cfg: oauthConfig{},
		cached: []*oauthSession{{
			ProjectID: "demo-project",
			Token: map[string]any{
				"location": "us-central1",
				"service_account": map[string]any{
					"type":                        "service_account",
					"project_id":                  "demo-project",
					"private_key_id":              "key-1",
					"private_key":                 string(pemKey),
					"client_email":                "svc@example.iam.gserviceaccount.com",
					"client_id":                   "1234567890",
					"token_uri":                   tokenServer.URL,
					"auth_uri":                    "https://accounts.google.com/o/oauth2/auth",
					"auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
					"client_x509_cert_url":        "https://example.com/cert",
				},
			},
		}},
	}

	resp, err := p.Chat(t.Context(), []Message{{Role: "user", Content: "hello"}}, nil, "gemini-2.5-pro", nil)
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
}
