package clawgoassets

import "embed"

// WorkspaceTemplates exposes the bundled workspace scaffold used by onboard.
//
//go:embed workspace
var WorkspaceTemplates embed.FS
