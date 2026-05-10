package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type unifiedBackupManifest struct {
	Version   int      `json:"version"`
	CreatedAt string   `json:"created_at"`
	Config    string   `json:"config"`
	Workspace string   `json:"workspace"`
	Includes  []string `json:"includes,omitempty"`
}

func backupCmd() {
	if len(os.Args) < 3 {
		backupHelp()
		return
	}
	switch strings.TrimSpace(os.Args[2]) {
	case "create":
		backupCreateCmd()
	case "import":
		backupImportCmd()
	default:
		fmt.Printf("Unknown backup command: %s\n", os.Args[2])
		backupHelp()
	}
}

func backupHelp() {
	fmt.Println("\nBackup commands:")
	fmt.Println("  create [archive.zip]    Create unified backup (config + sessions + memory + skills)")
	fmt.Println("  import <archive.zip>    Restore unified backup and auto-create rollback snapshot")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  clawgo backup create")
	fmt.Println("  clawgo backup create /tmp/clawgo-backup.zip")
	fmt.Println("  clawgo backup import /tmp/clawgo-backup.zip")
}

func backupCreateCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}
	out := ""
	if len(os.Args) >= 4 {
		out = strings.TrimSpace(os.Args[3])
	}
	if out == "" {
		out = defaultBackupPathForConfig("clawgo-backup", getConfigPath())
	}
	count, err := createUnifiedBackup(cfg.WorkspacePath(), getConfigPath(), out)
	if err != nil {
		fmt.Printf("Backup failed: %v\n", err)
		return
	}
	fmt.Printf("Backup created: %s (%d files)\n", out, count)
}

func backupImportCmd() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: clawgo backup import <archive.zip>")
		return
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}
	archive := strings.TrimSpace(os.Args[3])
	rollbackPath, restored, err := importUnifiedBackup(cfg.WorkspacePath(), getConfigPath(), archive)
	if err != nil {
		fmt.Printf("Import failed: %v\n", err)
		return
	}
	fmt.Printf("Import completed: %d files restored\n", restored)
	fmt.Printf("Rollback snapshot: %s\n", rollbackPath)
}

func defaultBackupPath(prefix string) string {
	return defaultBackupPathForConfig(prefix, getConfigPath())
}

func defaultBackupPathForConfig(prefix, configPath string) string {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		configPath = getConfigPath()
	}
	dir := filepath.Join(filepath.Dir(configPath), "backups")
	_ = os.MkdirAll(dir, 0755)
	name := fmt.Sprintf("%s-%s.zip", prefix, time.Now().Format("20060102-150405"))
	return filepath.Join(dir, name)
}

func createUnifiedBackup(workspacePath, configPath, archivePath string) (int, error) {
	workspacePath = strings.TrimSpace(workspacePath)
	configPath = strings.TrimSpace(configPath)
	archivePath = strings.TrimSpace(archivePath)
	if workspacePath == "" {
		return 0, fmt.Errorf("workspace path is empty")
	}
	if configPath == "" {
		return 0, fmt.Errorf("config path is empty")
	}
	if archivePath == "" {
		return 0, fmt.Errorf("archive path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(archivePath), 0755); err != nil {
		return 0, err
	}

	f, err := os.Create(archivePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()

	agentsRoot := filepath.Join(filepath.Dir(workspacePath), "agents")
	includes := []string{
		"config/config.json",
		"workspace/MEMORY.md",
		"workspace/memory/**",
		"workspace/skills/**",
		"agents/**",
		"workspace/AGENTS.md",
		"workspace/USER.md",
		"workspace/SOUL.md",
	}

	fileCount := 0
	seen := map[string]struct{}{}
	addFile := func(src, dst string) error {
		src = filepath.Clean(src)
		dst = filepath.ToSlash(strings.TrimSpace(dst))
		if src == "" || dst == "" {
			return nil
		}
		if _, ok := seen[dst]; ok {
			return nil
		}
		info, err := os.Stat(src)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		r, err := os.Open(src)
		if err != nil {
			return err
		}
		defer r.Close()
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = dst
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, r); err != nil {
			return err
		}
		seen[dst] = struct{}{}
		fileCount++
		return nil
	}
	addTree := func(srcDir, dstDir string) error {
		info, err := os.Stat(srcDir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			return addFile(srcDir, filepath.Join(dstDir, filepath.Base(srcDir)))
		}
		return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(srcDir, path)
			if err != nil {
				return err
			}
			return addFile(path, filepath.Join(dstDir, rel))
		})
	}

	if err := addFile(configPath, "config/config.json"); err != nil {
		return 0, err
	}
	if err := addFile(filepath.Join(workspacePath, "MEMORY.md"), "workspace/MEMORY.md"); err != nil {
		return 0, err
	}
	if err := addTree(filepath.Join(workspacePath, "memory"), "workspace/memory"); err != nil {
		return 0, err
	}
	if err := addTree(filepath.Join(workspacePath, "skills"), "workspace/skills"); err != nil {
		return 0, err
	}
	if err := addTree(agentsRoot, "agents"); err != nil {
		return 0, err
	}
	for _, name := range []string{"AGENTS.md", "USER.md", "SOUL.md"} {
		if err := addFile(filepath.Join(workspacePath, name), filepath.Join("workspace", name)); err != nil {
			return 0, err
		}
	}

	manifest := unifiedBackupManifest{
		Version:   1,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Config:    filepath.Clean(configPath),
		Workspace: filepath.Clean(workspacePath),
		Includes:  includes,
	}
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	w, err := zw.Create("manifest.json")
	if err != nil {
		return 0, err
	}
	if _, err := w.Write(manifestData); err != nil {
		return 0, err
	}
	return fileCount, nil
}

func importUnifiedBackup(workspacePath, configPath, archivePath string) (string, int, error) {
	workspacePath = strings.TrimSpace(workspacePath)
	configPath = strings.TrimSpace(configPath)
	archivePath = strings.TrimSpace(archivePath)
	if workspacePath == "" || configPath == "" || archivePath == "" {
		return "", 0, fmt.Errorf("invalid import paths")
	}
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", 0, err
	}
	defer r.Close()

	rollbackPath := defaultBackupPathForConfig("clawgo-rollback", configPath)
	if _, err := createUnifiedBackup(workspacePath, configPath, rollbackPath); err != nil {
		return "", 0, fmt.Errorf("create rollback snapshot: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "clawgo-import-*")
	if err != nil {
		return "", 0, err
	}
	defer os.RemoveAll(tmpDir)

	for _, zf := range r.File {
		target := filepath.Clean(filepath.Join(tmpDir, zf.Name))
		if !strings.HasPrefix(target, tmpDir+string(filepath.Separator)) && target != tmpDir {
			return "", 0, fmt.Errorf("invalid zip entry path: %s", zf.Name)
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", 0, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", 0, err
		}
		rc, err := zf.Open()
		if err != nil {
			return "", 0, err
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return "", 0, readErr
		}
		if err := os.WriteFile(target, data, zf.Mode()); err != nil {
			return "", 0, err
		}
	}

	agentsRoot := filepath.Join(filepath.Dir(workspacePath), "agents")
	restoreTasks := []struct {
		src string
		dst string
	}{
		{src: filepath.Join(tmpDir, "workspace"), dst: workspacePath},
		{src: filepath.Join(tmpDir, "agents"), dst: agentsRoot},
	}
	sort.SliceStable(restoreTasks, func(i, j int) bool { return restoreTasks[i].src < restoreTasks[j].src })
	restored := 0
	for _, task := range restoreTasks {
		info, err := os.Stat(task.src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return rollbackPath, restored, err
		}
		if !info.IsDir() {
			continue
		}
		if err := os.MkdirAll(task.dst, 0755); err != nil {
			return rollbackPath, restored, err
		}
		if err := copyDirectory(task.src, task.dst); err != nil {
			return rollbackPath, restored, err
		}
		filepath.Walk(task.src, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				restored++
			}
			return nil
		})
	}

	importedConfig := filepath.Join(tmpDir, "config", "config.json")
	if info, err := os.Stat(importedConfig); err == nil && !info.IsDir() {
		data, err := os.ReadFile(importedConfig)
		if err != nil {
			return rollbackPath, restored, err
		}
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return rollbackPath, restored, err
		}
		if err := os.WriteFile(configPath, data, 0644); err != nil {
			return rollbackPath, restored, err
		}
		restored++
	}

	return rollbackPath, restored, nil
}
