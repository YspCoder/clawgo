package nodes

import "time"

// Capability matrix reported by each node agent.
type Capabilities struct {
	Run      bool `json:"run"`
	Invoke   bool `json:"invoke"`
	Camera   bool `json:"camera"`
	Screen   bool `json:"screen"`
	Location bool `json:"location"`
	Canvas   bool `json:"canvas"`
}

// NodeInfo is the runtime descriptor for cross-device scheduling.
type NodeInfo struct {
	ID           string       `json:"id"`
	Name         string       `json:"name,omitempty"`
	OS           string       `json:"os,omitempty"`
	Arch         string       `json:"arch,omitempty"`
	Version      string       `json:"version,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	LastSeenAt   time.Time    `json:"last_seen_at"`
	Online       bool         `json:"online"`
}

// Envelope for node commands.
type Request struct {
	Action string                 `json:"action"`
	Node   string                 `json:"node,omitempty"`
	Args   map[string]interface{} `json:"args,omitempty"`
}

// Envelope for node responses.
type Response struct {
	OK      bool                   `json:"ok"`
	Error   string                 `json:"error,omitempty"`
	Node    string                 `json:"node,omitempty"`
	Action  string                 `json:"action,omitempty"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}
