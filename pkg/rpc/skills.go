package rpc

import "context"

type SkillsService interface {
	View(context.Context, SkillsViewRequest) (*SkillsViewResponse, *Error)
	Mutate(context.Context, SkillsMutateRequest) (*SkillsMutateResponse, *Error)
}

type SkillsViewRequest struct {
	ID           string `json:"id,omitempty"`
	File         string `json:"file,omitempty"`
	Files        bool   `json:"files,omitempty"`
	CheckUpdates bool   `json:"check_updates,omitempty"`
}

type SkillsViewItem struct {
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

type SkillsViewResponse struct {
	ID               string           `json:"id,omitempty"`
	File             string           `json:"file,omitempty"`
	Content          string           `json:"content,omitempty"`
	FilesList        []string         `json:"files,omitempty"`
	Skills           []SkillsViewItem `json:"skills,omitempty"`
	Source           string           `json:"source,omitempty"`
	ClawhubInstalled bool             `json:"clawhub_installed,omitempty"`
	ClawhubPath      string           `json:"clawhub_path,omitempty"`
}

type SkillsMutateRequest struct {
	Action           string   `json:"action"`
	ID               string   `json:"id,omitempty"`
	Name             string   `json:"name,omitempty"`
	Description      string   `json:"description,omitempty"`
	SystemPrompt     string   `json:"system_prompt,omitempty"`
	Tools            []string `json:"tools,omitempty"`
	IgnoreSuspicious bool     `json:"ignore_suspicious,omitempty"`
	File             string   `json:"file,omitempty"`
	Content          string   `json:"content,omitempty"`
}

type SkillsMutateResponse struct {
	Installed   string   `json:"installed,omitempty"`
	InstalledOK bool     `json:"installed_ok,omitempty"`
	Output      string   `json:"output,omitempty"`
	ClawhubPath string   `json:"clawhub_path,omitempty"`
	Name        string   `json:"name,omitempty"`
	File        string   `json:"file,omitempty"`
	Deleted     bool     `json:"deleted,omitempty"`
	ID          string   `json:"id,omitempty"`
	Imported    []string `json:"imported,omitempty"`
}
