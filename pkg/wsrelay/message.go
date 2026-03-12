package wsrelay

// Message represents the JSON payload exchanged with websocket clients.
type Message struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload,omitempty"`
}

const (
	MessageTypeHTTPReq     = "http_request"
	MessageTypeHTTPResp    = "http_response"
	MessageTypeStreamStart = "stream_start"
	MessageTypeStreamChunk = "stream_chunk"
	MessageTypeStreamEnd   = "stream_end"
	MessageTypeError       = "error"
	MessageTypePing        = "ping"
	MessageTypePong        = "pong"
)
