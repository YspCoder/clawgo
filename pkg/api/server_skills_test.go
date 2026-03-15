package api

import (
	"archive/zip"
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportSkillArchiveFromMultipartSucceedsForSmallArchive(t *testing.T) {
	t.Parallel()

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	entry, err := zw.Create("demo/SKILL.md")
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if _, err := entry.Write([]byte("# Demo\n")); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	req := multipartRequest(t, "demo.zip", archive.Bytes())
	skillsDir := t.TempDir()
	imported, err := importSkillArchiveFromMultipart(req, skillsDir)
	if err != nil {
		t.Fatalf("import archive: %v", err)
	}
	if len(imported) != 1 || imported[0] != "demo" {
		t.Fatalf("unexpected imported skills: %+v", imported)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "demo", "SKILL.md")); err != nil {
		t.Fatalf("expected imported SKILL.md: %v", err)
	}
}

func TestExtractArchiveRejectsOversizedExpandedEntry(t *testing.T) {
	t.Parallel()

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	entry, err := zw.Create("demo/SKILL.md")
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if _, err := entry.Write(bytes.Repeat([]byte("a"), int(skillArchiveMaxSingleFile+1))); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "demo.zip")
	if err := os.WriteFile(archivePath, archive.Bytes(), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	err = extractArchive(archivePath, t.TempDir(), &archiveExtractLimits{
		maxFiles:      skillArchiveMaxFiles,
		maxSingleFile: skillArchiveMaxSingleFile,
		maxExpanded:   skillArchiveMaxExpanded,
	})
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

func multipartRequest(t *testing.T, filename string, body []byte) *http.Request {
	t.Helper()
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest("POST", "/api/skills", &form)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.ContentLength = int64(form.Len())
	return req
}
