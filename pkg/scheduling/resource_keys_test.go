package scheduling

import "testing"

func TestExtractResourceKeysDirective(t *testing.T) {
	keys, cleaned, ok := ExtractResourceKeysDirective("[resource_keys: repo:acme/app,file:pkg/a.go]\nplease check")
	if !ok {
		t.Fatalf("expected directive")
	}
	if len(keys) != 2 || keys[0] != "repo:acme/app" || keys[1] != "file:pkg/a.go" {
		t.Fatalf("unexpected keys: %#v", keys)
	}
	if cleaned != "please check" {
		t.Fatalf("unexpected cleaned content: %q", cleaned)
	}
}

func TestDeriveResourceKeysHeuristic(t *testing.T) {
	keys := DeriveResourceKeys("update pkg/agent/loop.go on main")
	if len(keys) == 0 {
		t.Fatalf("expected non-empty keys")
	}
	foundFile := false
	foundBranch := false
	for _, k := range keys {
		if k == "file:pkg/agent/loop.go" {
			foundFile = true
		}
		if k == "branch:main" {
			foundBranch = true
		}
	}
	if !foundFile {
		t.Fatalf("expected file key in %#v", keys)
	}
	if !foundBranch {
		t.Fatalf("expected branch key in %#v", keys)
	}
}

func TestParseResourceKeyListAddsFilePrefix(t *testing.T) {
	keys := ParseResourceKeyList("pkg/a.go, repo:acme/app")
	if len(keys) != 2 {
		t.Fatalf("unexpected len: %#v", keys)
	}
	if keys[0] != "file:pkg/a.go" && keys[1] != "file:pkg/a.go" {
		t.Fatalf("expected file-prefixed key in %#v", keys)
	}
}
