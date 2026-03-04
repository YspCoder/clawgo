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
	for _, k := range keys {
		if k == "repo:default" {
			t.Fatalf("should not include global repo:default key in %#v", keys)
		}
	}
}

func TestDeriveResourceKeysNaturalLanguageTopic(t *testing.T) {
	keys := DeriveResourceKeys("请更新webui交互并补充readme文档")
	if len(keys) == 0 {
		t.Fatalf("expected non-empty keys")
	}
	foundWebUI := false
	foundDocs := false
	for _, k := range keys {
		if k == "topic:webui" {
			foundWebUI = true
		}
		if k == "topic:docs" {
			foundDocs = true
		}
	}
	if !foundWebUI || !foundDocs {
		t.Fatalf("expected topic keys in %#v", keys)
	}
}

func TestDeriveResourceKeysNaturalLanguageFallbackGeneral(t *testing.T) {
	keys := DeriveResourceKeys("帮我处理一下")
	if len(keys) != 1 || keys[0] != "scope:general" {
		t.Fatalf("expected scope:general fallback, got %#v", keys)
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
