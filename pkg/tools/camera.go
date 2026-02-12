package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type CameraTool struct {
	workspace string
}

func NewCameraTool(workspace string) *CameraTool {
	return &CameraTool{
		workspace: workspace,
	}
}

func (t *CameraTool) Name() string {
	return "camera_snap"
}

func (t *CameraTool) Description() string {
	return "Take a photo using the system camera (/dev/video0) and save to workspace."
}

func (t *CameraTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"filename": map[string]interface{}{
				"type":        "string",
				"description": "Optional filename (default: snap_TIMESTAMP.jpg)",
			},
		},
	}
}

func (t *CameraTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	filename := ""
	if v, ok := args["filename"].(string); ok && v != "" {
		filename = v
	} else {
		filename = fmt.Sprintf("snap_%d.jpg", time.Now().Unix())
	}
	
	// Ensure filename is safe and within workspace
	filename = filepath.Clean(filename)
	if filepath.IsAbs(filename) {
		return "", fmt.Errorf("filename must be relative to workspace")
	}
	
	outputPath := filepath.Join(t.workspace, filename)

	// Check if video device exists
	if _, err := os.Stat("/dev/video0"); os.IsNotExist(err) {
		return "", fmt.Errorf("camera device /dev/video0 not found")
	}

	// ffmpeg -y -f video4linux2 -i /dev/video0 -vframes 1 -q:v 2 output.jpg
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-f", "video4linux2", "-i", "/dev/video0", "-vframes", "1", "-q:v", "2", outputPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Error taking photo: %v\nOutput: %s", err, string(output)), nil
	}

	return fmt.Sprintf("Photo saved to %s", filename), nil
}
