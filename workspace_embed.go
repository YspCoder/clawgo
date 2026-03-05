package clawgo

import "embed"

// EmbeddedWorkspace bundles onboarding workspace templates into the binary.
//go:embed all:workspace
var EmbeddedWorkspace embed.FS

