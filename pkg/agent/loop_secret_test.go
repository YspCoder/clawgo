package agent

import "testing"

func TestRedactSecretsInText(t *testing.T) {
	in := `{"token":"abc123","authorization":"Bearer sk-xyz","password":"p@ss","note":"ok"}`
	out := redactSecretsInText(in)

	if out == in {
		t.Fatalf("expected redaction to change input")
	}
	if containsAnySubstring(out, "abc123", "sk-xyz", "p@ss") {
		t.Fatalf("expected sensitive values to be redacted, got: %s", out)
	}
}

func TestSanitizeSensitiveToolArgs(t *testing.T) {
	args := map[string]interface{}{
		"command": "git -c http.extraHeader='Authorization: token abc123' ls-remote https://example.com/repo.git",
		"token":   "abc123",
	}
	safe := sanitizeSensitiveToolArgs(args)
	if safe["token"] == "abc123" {
		t.Fatalf("expected token field to be redacted")
	}
}
