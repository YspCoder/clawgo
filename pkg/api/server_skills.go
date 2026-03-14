package api

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/tools"
)

func (s *Server) handleWebUISkills(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	skillsDir := filepath.Join(s.workspacePath, "skills")
	if strings.TrimSpace(skillsDir) == "" {
		http.Error(w, "workspace not configured", http.StatusInternalServerError)
		return
	}
	_ = os.MkdirAll(skillsDir, 0755)

	resolveSkillPath := func(name string) (string, error) {
		name = strings.TrimSpace(name)
		if name == "" {
			return "", fmt.Errorf("name required")
		}
		cands := []string{
			filepath.Join(skillsDir, name),
			filepath.Join(skillsDir, name+".disabled"),
			filepath.Join("/root/clawgo/workspace/skills", name),
			filepath.Join("/root/clawgo/workspace/skills", name+".disabled"),
		}
		for _, p := range cands {
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				return p, nil
			}
		}
		return "", fmt.Errorf("skill not found: %s", name)
	}

	switch r.Method {
	case http.MethodGet:
		clawhubPath := strings.TrimSpace(resolveClawHubBinary(r.Context()))
		clawhubInstalled := clawhubPath != ""
		if id := strings.TrimSpace(r.URL.Query().Get("id")); id != "" {
			skillPath, err := resolveSkillPath(id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			if strings.TrimSpace(r.URL.Query().Get("files")) == "1" {
				var files []string
				_ = filepath.WalkDir(skillPath, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return nil
					}
					if d.IsDir() {
						return nil
					}
					rel, _ := filepath.Rel(skillPath, path)
					if strings.HasPrefix(rel, "..") {
						return nil
					}
					files = append(files, filepath.ToSlash(rel))
					return nil
				})
				writeJSON(w, map[string]interface{}{"ok": true, "id": id, "files": files})
				return
			}
			if f := strings.TrimSpace(r.URL.Query().Get("file")); f != "" {
				clean, content, found, err := readRelativeTextFile(skillPath, f)
				if err != nil {
					http.Error(w, err.Error(), relativeFilePathStatus(err))
					return
				}
				if !found {
					http.Error(w, os.ErrNotExist.Error(), http.StatusInternalServerError)
					return
				}
				writeJSON(w, map[string]interface{}{"ok": true, "id": id, "file": filepath.ToSlash(clean), "content": content})
				return
			}
		}

		type skillItem struct {
			ID            string   `json:"id"`
			Name          string   `json:"name"`
			Description   string   `json:"description"`
			Tools         []string `json:"tools"`
			SystemPrompt  string   `json:"system_prompt,omitempty"`
			Enabled       bool     `json:"enabled"`
			UpdateChecked bool     `json:"update_checked"`
			RemoteFound   bool     `json:"remote_found,omitempty"`
			RemoteVersion string   `json:"remote_version,omitempty"`
			CheckError    string   `json:"check_error,omitempty"`
			Source        string   `json:"source,omitempty"`
		}
		candDirs := []string{skillsDir, filepath.Join("/root/clawgo/workspace", "skills")}
		seenDirs := map[string]struct{}{}
		seenSkills := map[string]struct{}{}
		items := make([]skillItem, 0)
		checkUpdates := strings.TrimSpace(r.URL.Query().Get("check_updates")) == "1"

		for _, dir := range candDirs {
			dir = strings.TrimSpace(dir)
			if dir == "" {
				continue
			}
			if _, ok := seenDirs[dir]; ok {
				continue
			}
			seenDirs[dir] = struct{}{}
			entries, err := os.ReadDir(dir)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				name := e.Name()
				enabled := !strings.HasSuffix(name, ".disabled")
				baseName := strings.TrimSuffix(name, ".disabled")
				if _, ok := seenSkills[baseName]; ok {
					continue
				}
				seenSkills[baseName] = struct{}{}
				desc, skillTools, sys := readSkillMeta(filepath.Join(dir, name, "SKILL.md"))
				if desc == "" || len(skillTools) == 0 || sys == "" {
					d2, t2, s2 := readSkillMeta(filepath.Join(dir, baseName, "SKILL.md"))
					if desc == "" {
						desc = d2
					}
					if len(skillTools) == 0 {
						skillTools = t2
					}
					if sys == "" {
						sys = s2
					}
				}
				if skillTools == nil {
					skillTools = []string{}
				}
				it := skillItem{ID: baseName, Name: baseName, Description: desc, Tools: skillTools, SystemPrompt: sys, Enabled: enabled, UpdateChecked: checkUpdates && clawhubInstalled, Source: dir}
				if checkUpdates && clawhubInstalled {
					found, version, checkErr := queryClawHubSkillVersion(r.Context(), baseName)
					it.RemoteFound = found
					it.RemoteVersion = version
					if checkErr != nil {
						it.CheckError = checkErr.Error()
					}
				}
				items = append(items, it)
			}
		}
		writeJSON(w, map[string]interface{}{
			"ok":                true,
			"skills":            items,
			"source":            "clawhub",
			"clawhub_installed": clawhubInstalled,
			"clawhub_path":      clawhubPath,
		})

	case http.MethodPost:
		ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
		if strings.Contains(ct, "multipart/form-data") {
			imported, err := importSkillArchiveFromMultipart(r, skillsDir)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "imported": imported})
			return
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action := strings.ToLower(stringFromMap(body, "action"))
		if action == "install_clawhub" {
			output, err := ensureClawHubReady(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]interface{}{
				"ok":           true,
				"output":       output,
				"installed":    true,
				"clawhub_path": resolveClawHubBinary(r.Context()),
			})
			return
		}
		id := stringFromMap(body, "id")
		name := strings.TrimSpace(firstNonEmptyString(stringFromMap(body, "name"), id))
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		enabledPath := filepath.Join(skillsDir, name)
		disabledPath := enabledPath + ".disabled"
		type skillActionHandler func() bool
		handlers := map[string]skillActionHandler{
			"install": func() bool {
				clawhubPath := strings.TrimSpace(resolveClawHubBinary(r.Context()))
				if clawhubPath == "" {
					http.Error(w, "clawhub is not installed. please install clawhub first.", http.StatusPreconditionFailed)
					return false
				}
				ignoreSuspicious, _ := tools.MapBoolArg(body, "ignore_suspicious")
				args := []string{"install", name}
				if ignoreSuspicious {
					args = append(args, "--force")
				}
				cmd := exec.CommandContext(r.Context(), clawhubPath, args...)
				cmd.Dir = strings.TrimSpace(s.workspacePath)
				out, err := cmd.CombinedOutput()
				if err != nil {
					outText := string(out)
					lower := strings.ToLower(outText)
					if strings.Contains(lower, "rate limit exceeded") || strings.Contains(lower, "too many requests") {
						http.Error(w, fmt.Sprintf("clawhub rate limit exceeded. please retry later or configure auth token.\n%s", outText), http.StatusTooManyRequests)
						return false
					}
					http.Error(w, fmt.Sprintf("install failed: %v\n%s", err, outText), http.StatusInternalServerError)
					return false
				}
				writeJSON(w, map[string]interface{}{"ok": true, "installed": name, "output": string(out)})
				return true
			},
			"enable": func() bool {
				if _, err := os.Stat(disabledPath); err == nil {
					if err := os.Rename(disabledPath, enabledPath); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return false
					}
				}
				writeJSON(w, map[string]interface{}{"ok": true})
				return true
			},
			"disable": func() bool {
				if _, err := os.Stat(enabledPath); err == nil {
					if err := os.Rename(enabledPath, disabledPath); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return false
					}
				}
				writeJSON(w, map[string]interface{}{"ok": true})
				return true
			},
			"write_file": func() bool {
				skillPath, err := resolveSkillPath(name)
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return false
				}
				content := rawStringFromMap(body, "content")
				filePath := stringFromMap(body, "file")
				clean, err := writeRelativeTextFile(skillPath, filePath, content, true)
				if err != nil {
					http.Error(w, err.Error(), relativeFilePathStatus(err))
					return false
				}
				writeJSON(w, map[string]interface{}{"ok": true, "name": name, "file": filepath.ToSlash(clean)})
				return true
			},
			"create": func() bool {
				return createOrUpdateSkill(w, enabledPath, name, body, true)
			},
			"update": func() bool {
				return createOrUpdateSkill(w, enabledPath, name, body, false)
			},
		}
		if handler := handlers[action]; handler != nil {
			handler()
			return
		}
		http.Error(w, "unsupported action", http.StatusBadRequest)

	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		pathA := filepath.Join(skillsDir, id)
		pathB := pathA + ".disabled"
		deleted := false
		if err := os.RemoveAll(pathA); err == nil {
			deleted = true
		}
		if err := os.RemoveAll(pathB); err == nil {
			deleted = true
		}
		writeJSON(w, map[string]interface{}{"ok": true, "deleted": deleted, "id": id})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func buildSkillMarkdown(name, desc string, toolsList []string, systemPrompt string) string {
	if desc == "" {
		desc = "No description provided."
	}
	if len(toolsList) == 0 {
		toolsList = []string{""}
	}
	toolLines := make([]string, 0, len(toolsList))
	for _, t := range toolsList {
		if t == "" {
			continue
		}
		toolLines = append(toolLines, "- "+t)
	}
	if len(toolLines) == 0 {
		toolLines = []string{"- (none)"}
	}
	return fmt.Sprintf(`---
name: %s
description: %s
---

# %s

%s

## Tools
%s

## System Prompt
%s
`, name, desc, name, desc, strings.Join(toolLines, "\n"), systemPrompt)
}

func createOrUpdateSkill(w http.ResponseWriter, enabledPath, name string, body map[string]interface{}, checkExists bool) bool {
	desc := rawStringFromMap(body, "description")
	sys := rawStringFromMap(body, "system_prompt")
	toolsList := stringListFromMap(body, "tools")
	if checkExists {
		if _, err := os.Stat(enabledPath); err == nil {
			http.Error(w, "skill already exists", http.StatusBadRequest)
			return false
		}
	}
	if err := os.MkdirAll(filepath.Join(enabledPath, "scripts"), 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	skillMD := buildSkillMarkdown(name, desc, toolsList, sys)
	if err := os.WriteFile(filepath.Join(enabledPath, "SKILL.md"), []byte(skillMD), 0644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	writeJSON(w, map[string]interface{}{"ok": true})
	return true
}

func readSkillMeta(path string) (desc string, toolsList []string, systemPrompt string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", []string{}, ""
	}
	s := string(b)
	reDesc := regexp.MustCompile(`(?m)^description:\s*(.+)$`)
	reTools := regexp.MustCompile(`(?m)^##\s*Tools\s*$`)
	rePrompt := regexp.MustCompile(`(?m)^##\s*System Prompt\s*$`)
	if m := reDesc.FindStringSubmatch(s); len(m) > 1 {
		desc = m[1]
	}
	if loc := reTools.FindStringIndex(s); loc != nil {
		block := s[loc[1]:]
		if p := rePrompt.FindStringIndex(block); p != nil {
			block = block[:p[0]]
		}
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimPrefix(line, "-")
			if line != "" {
				toolsList = append(toolsList, line)
			}
		}
	}
	if toolsList == nil {
		toolsList = []string{}
	}
	if loc := rePrompt.FindStringIndex(s); loc != nil {
		systemPrompt = s[loc[1]:]
	}
	return
}

func queryClawHubSkillVersion(ctx context.Context, skill string) (found bool, version string, err error) {
	if skill == "" {
		return false, "", fmt.Errorf("skill empty")
	}
	clawhubPath := strings.TrimSpace(resolveClawHubBinary(ctx))
	if clawhubPath == "" {
		return false, "", fmt.Errorf("clawhub not installed")
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, clawhubPath, "search", skill, "--json")
	out, runErr := cmd.Output()
	if runErr != nil {
		return false, "", runErr
	}
	var payload interface{}
	if err := json.Unmarshal(out, &payload); err != nil {
		return false, "", err
	}
	lowerSkill := strings.ToLower(skill)
	var walk func(v interface{}) (bool, string)
	walk = func(v interface{}) (bool, string) {
		switch t := v.(type) {
		case map[string]interface{}:
			name := strings.ToLower(strings.TrimSpace(anyToString(t["name"])))
			if name == "" {
				name = strings.ToLower(strings.TrimSpace(anyToString(t["id"])))
			}
			if name == lowerSkill || strings.Contains(name, lowerSkill) {
				ver := anyToString(t["version"])
				if ver == "" {
					ver = anyToString(t["latest_version"])
				}
				return true, ver
			}
			for _, vv := range t {
				if ok, ver := walk(vv); ok {
					return ok, ver
				}
			}
		case []interface{}:
			for _, vv := range t {
				if ok, ver := walk(vv); ok {
					return ok, ver
				}
			}
		}
		return false, ""
	}
	ok, ver := walk(payload)
	return ok, ver, nil
}

func ensureClawHubReady(ctx context.Context) (string, error) {
	outs := make([]string, 0, 4)
	if p := resolveClawHubBinary(ctx); p != "" {
		return "clawhub already installed at: " + p, nil
	}
	nodeOut, err := ensureNodeRuntime(ctx)
	if nodeOut != "" {
		outs = append(outs, nodeOut)
	}
	if err != nil {
		return strings.Join(outs, "\n"), err
	}
	clawOut, err := runInstallCommand(ctx, "npm i -g clawhub")
	if clawOut != "" {
		outs = append(outs, clawOut)
	}
	if err != nil {
		return strings.Join(outs, "\n"), err
	}
	if p := resolveClawHubBinary(ctx); p != "" {
		outs = append(outs, "clawhub installed at: "+p)
		return strings.Join(outs, "\n"), nil
	}
	return strings.Join(outs, "\n"), fmt.Errorf("installed clawhub but executable still not found in PATH")
}

func importSkillArchiveFromMultipart(r *http.Request, skillsDir string) ([]string, error) {
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		return nil, err
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("file required")
	}
	defer f.Close()

	uploadDir := filepath.Join(os.TempDir(), "clawgo_skill_uploads")
	_ = os.MkdirAll(uploadDir, 0755)
	archivePath := filepath.Join(uploadDir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(h.Filename)))
	out, err := os.Create(archivePath)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(out, f); err != nil {
		_ = out.Close()
		_ = os.Remove(archivePath)
		return nil, err
	}
	_ = out.Close()
	defer os.Remove(archivePath)

	extractDir, err := os.MkdirTemp("", "clawgo_skill_extract_*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(extractDir)

	if err := extractArchive(archivePath, extractDir); err != nil {
		return nil, err
	}

	type candidate struct {
		name string
		dir  string
	}
	candidates := make([]candidate, 0)
	seen := map[string]struct{}{}
	err = filepath.WalkDir(extractDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), "SKILL.md") {
			dir := filepath.Dir(path)
			rel, relErr := filepath.Rel(extractDir, dir)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(strings.TrimSpace(rel))
			if rel == "" {
				rel = "."
			}
			name := filepath.Base(rel)
			if rel == "." {
				name = archiveBaseName(h.Filename)
			}
			name = sanitizeSkillName(name)
			if name == "" {
				return nil
			}
			if _, ok := seen[name]; ok {
				return nil
			}
			seen[name] = struct{}{}
			candidates = append(candidates, candidate{name: name, dir: dir})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no SKILL.md found in archive")
	}

	imported := make([]string, 0, len(candidates))
	for _, c := range candidates {
		dst := filepath.Join(skillsDir, c.name)
		if _, err := os.Stat(dst); err == nil {
			return nil, fmt.Errorf("skill already exists: %s", c.name)
		}
		if _, err := os.Stat(dst + ".disabled"); err == nil {
			return nil, fmt.Errorf("disabled skill already exists: %s", c.name)
		}
		if err := copyDir(c.dir, dst); err != nil {
			return nil, err
		}
		imported = append(imported, c.name)
	}
	sort.Strings(imported)
	return imported, nil
}

func archiveBaseName(filename string) string {
	name := filepath.Base(strings.TrimSpace(filename))
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return name[:len(name)-len(".tar.gz")]
	case strings.HasSuffix(lower, ".tgz"):
		return name[:len(name)-len(".tgz")]
	case strings.HasSuffix(lower, ".zip"):
		return name[:len(name)-len(".zip")]
	case strings.HasSuffix(lower, ".tar"):
		return name[:len(name)-len(".tar")]
	default:
		ext := filepath.Ext(name)
		return strings.TrimSuffix(name, ext)
	}
}

func sanitizeSkillName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, ch := range strings.ToLower(name) {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			b.WriteRune(ch)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" || out == "." {
		return ""
	}
	return out
}

func extractArchive(archivePath, targetDir string) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, targetDir)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archivePath, targetDir)
	case strings.HasSuffix(lower, ".tar"):
		return extractTar(archivePath, targetDir)
	default:
		return fmt.Errorf("unsupported archive format: %s", filepath.Base(archivePath))
	}
}

func extractZip(archivePath, targetDir string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if err := writeArchivedEntry(targetDir, f.Name, f.FileInfo().IsDir(), func() (io.ReadCloser, error) {
			return f.Open()
		}); err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(archivePath, targetDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	return extractTarReader(tar.NewReader(gz), targetDir)
}

func extractTar(archivePath, targetDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	return extractTarReader(tar.NewReader(f), targetDir)
}

func extractTarReader(tr *tar.Reader, targetDir string) error {
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := writeArchivedEntry(targetDir, hdr.Name, true, nil); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			name := hdr.Name
			if err := writeArchivedEntry(targetDir, name, false, func() (io.ReadCloser, error) {
				return io.NopCloser(tr), nil
			}); err != nil {
				return err
			}
		}
	}
}

func writeArchivedEntry(targetDir, name string, isDir bool, opener func() (io.ReadCloser, error)) error {
	clean := filepath.Clean(strings.TrimSpace(name))
	clean = strings.TrimPrefix(clean, string(filepath.Separator))
	clean = strings.TrimPrefix(clean, "/")
	for strings.HasPrefix(clean, "../") {
		clean = strings.TrimPrefix(clean, "../")
	}
	if clean == "." || clean == "" {
		return nil
	}
	dst := filepath.Join(targetDir, clean)
	absTarget, _ := filepath.Abs(targetDir)
	absDst, _ := filepath.Abs(dst)
	if !strings.HasPrefix(absDst, absTarget+string(filepath.Separator)) && absDst != absTarget {
		return fmt.Errorf("invalid archive entry path: %s", name)
	}
	if isDir {
		return os.MkdirAll(dst, 0755)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	rc, err := opener()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		info, err := e.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		in, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		out, err := os.Create(dstPath)
		if err != nil {
			_ = in.Close()
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			_ = in.Close()
			return err
		}
		_ = out.Close()
		_ = in.Close()
	}
	return nil
}
