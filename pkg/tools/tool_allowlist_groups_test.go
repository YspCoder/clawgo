package tools

import "testing"

func TestExpandToolAllowlistEntries_GroupPrefix(t *testing.T) {
	got := ExpandToolAllowlistEntries([]string{"group:files_read"})
	contains := map[string]bool{}
	for _, item := range got {
		contains[item] = true
	}
	if !contains["read_file"] || !contains["list_dir"] {
		t.Fatalf("files_read group expansion missing expected tools: %v", got)
	}
	if contains["write_file"] {
		t.Fatalf("files_read group should not include write_file: %v", got)
	}
}

func TestExpandToolAllowlistEntries_BareGroupAndAlias(t *testing.T) {
	got := ExpandToolAllowlistEntries([]string{"memory_all", "@agents", "skill"})
	contains := map[string]bool{}
	for _, item := range got {
		contains[item] = true
	}
	if !contains["memory_search"] || !contains["memory_write"] {
		t.Fatalf("memory_all expansion missing memory tools: %v", got)
	}
	if !contains["world"] || !contains["agent_profile"] {
		t.Fatalf("agents alias expansion missing world tools: %v", got)
	}
	if !contains["skill_exec"] {
		t.Fatalf("skills alias expansion missing skill_exec: %v", got)
	}
}
