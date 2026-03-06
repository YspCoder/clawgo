package agent

import "testing"

func TestSplitPlannedSegmentsDoesNotSplitPlainNewlines(t *testing.T) {
	t.Parallel()

	content := "编写ai漫画创作平台demo\n让产品出方案，方案出完让前端后端开始编写，写完后交个测试过一下"
	got := splitPlannedSegments(content)
	if len(got) != 1 {
		t.Fatalf("expected 1 segment, got %d: %#v", len(got), got)
	}
}

func TestSplitPlannedSegmentsStillSplitsBullets(t *testing.T) {
	t.Parallel()

	content := "1. 先实现前端\n2. 再补测试"
	got := splitPlannedSegments(content)
	if len(got) != 2 {
		t.Fatalf("expected 2 segments, got %d: %#v", len(got), got)
	}
}

func TestSplitPlannedSegmentsStillSplitsSemicolons(t *testing.T) {
	t.Parallel()

	content := "先实现前端；再补测试"
	got := splitPlannedSegments(content)
	if len(got) != 2 {
		t.Fatalf("expected 2 segments, got %d: %#v", len(got), got)
	}
}
