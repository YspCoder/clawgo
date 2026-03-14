package rpc

import (
	"encoding/json"
	"strings"
)

type Request struct {
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
}

type Response struct {
	OK        bool        `json:"ok"`
	Result    interface{} `json:"result,omitempty"`
	Error     *Error      `json:"error,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
}

type Error struct {
	Code      string      `json:"code"`
	Message   string      `json:"message"`
	Details   interface{} `json:"details,omitempty"`
	Retryable bool        `json:"retryable,omitempty"`
}

func (r Request) NormalizedMethod() string {
	return strings.ToLower(strings.TrimSpace(r.Method))
}

func DecodeParams(raw json.RawMessage, target interface{}) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, target)
}
