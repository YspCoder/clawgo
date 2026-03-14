package rpc

import "context"

type WorkspaceService interface {
	ListFiles(context.Context, ListWorkspaceFilesRequest) (*ListWorkspaceFilesResponse, *Error)
	ReadFile(context.Context, ReadWorkspaceFileRequest) (*ReadWorkspaceFileResponse, *Error)
	WriteFile(context.Context, WriteWorkspaceFileRequest) (*WriteWorkspaceFileResponse, *Error)
	DeleteFile(context.Context, DeleteWorkspaceFileRequest) (*DeleteWorkspaceFileResponse, *Error)
}

type ListWorkspaceFilesRequest struct {
	Scope string `json:"scope,omitempty"`
}

type ListWorkspaceFilesResponse struct {
	Files []string `json:"files"`
}

type ReadWorkspaceFileRequest struct {
	Scope string `json:"scope,omitempty"`
	Path  string `json:"path"`
}

type ReadWorkspaceFileResponse struct {
	Path    string `json:"path,omitempty"`
	Found   bool   `json:"found,omitempty"`
	Content string `json:"content,omitempty"`
}

type WriteWorkspaceFileRequest struct {
	Scope   string `json:"scope,omitempty"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

type WriteWorkspaceFileResponse struct {
	Path  string `json:"path,omitempty"`
	Saved bool   `json:"saved,omitempty"`
}

type DeleteWorkspaceFileRequest struct {
	Scope string `json:"scope,omitempty"`
	Path  string `json:"path"`
}

type DeleteWorkspaceFileResponse struct {
	Path    string `json:"path,omitempty"`
	Deleted bool   `json:"deleted"`
}
