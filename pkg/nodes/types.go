package nodes

import "time"

// Capability matrix reported by each node agent.
type Capabilities struct {
	Run      bool `json:"run"`
	Invoke   bool `json:"invoke"`
	Model    bool `json:"model"`
	Camera   bool `json:"camera"`
	Screen   bool `json:"screen"`
	Location bool `json:"location"`
	Canvas   bool `json:"canvas"`
}

// AgentInfo describes an enabled agent exposed by a remote clawgo node.
type AgentInfo struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name,omitempty"`
	Role          string `json:"role,omitempty"`
	Type          string `json:"type,omitempty"`
	Transport     string `json:"transport,omitempty"`
	ParentAgentID string `json:"parent_agent_id,omitempty"`
}

// Artifact describes a file/media payload returned from a node action.
type Artifact struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Kind        string `json:"kind,omitempty"`
	MIMEType    string `json:"mime_type,omitempty"`
	Storage     string `json:"storage,omitempty"`
	Path        string `json:"path,omitempty"`
	URL         string `json:"url,omitempty"`
	ContentText string `json:"content_text,omitempty"`
	ContentB64  string `json:"content_base64,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	SourcePath  string `json:"source_path,omitempty"`
}

// NodeInfo is the runtime descriptor for cross-device scheduling.
type NodeInfo struct {
	ID           string       `json:"id"`
	Name         string       `json:"name,omitempty"`
	OS           string       `json:"os,omitempty"`
	Arch         string       `json:"arch,omitempty"`
	Version      string       `json:"version,omitempty"`
	Endpoint     string       `json:"endpoint,omitempty"`
	Token        string       `json:"token,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	Actions      []string     `json:"actions,omitempty"`
	Models       []string     `json:"models,omitempty"`
	Agents       []AgentInfo  `json:"agents,omitempty"`
	RegisteredAt time.Time    `json:"registered_at,omitempty"`
	LastSeenAt   time.Time    `json:"last_seen_at"`
	Online       bool         `json:"online"`
}

// Envelope for node commands.
type Request struct {
	Action string                 `json:"action"`
	Node   string                 `json:"node,omitempty"`
	Task   string                 `json:"task,omitempty"`
	Model  string                 `json:"model,omitempty"`
	Args   map[string]interface{} `json:"args,omitempty"`
}

// Envelope for node responses.
type Response struct {
	OK      bool                   `json:"ok"`
	Code    string                 `json:"code,omitempty"`
	Error   string                 `json:"error,omitempty"`
	Node    string                 `json:"node,omitempty"`
	Action  string                 `json:"action,omitempty"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

// WireMessage is the websocket envelope for node lifecycle messages.
type WireMessage struct {
	Type     string                 `json:"type"`
	ID       string                 `json:"id,omitempty"`
	From     string                 `json:"from,omitempty"`
	To       string                 `json:"to,omitempty"`
	Session  string                 `json:"session,omitempty"`
	Node     *NodeInfo              `json:"node,omitempty"`
	Request  *Request               `json:"request,omitempty"`
	Response *Response              `json:"response,omitempty"`
	Payload  map[string]interface{} `json:"payload,omitempty"`
}

// WireAck is the websocket response envelope for node lifecycle messages.
type WireAck struct {
	OK    bool   `json:"ok"`
	Type  string `json:"type"`
	ID    string `json:"id,omitempty"`
	Error string `json:"error,omitempty"`
}
