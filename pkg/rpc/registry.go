package rpc

import (
	"context"
	"encoding/json"
	"strings"
)

type MethodHandler func(context.Context, json.RawMessage) (interface{}, *Error)

type Registry struct {
	methods map[string]MethodHandler
}

func NewRegistry() *Registry {
	return &Registry{methods: map[string]MethodHandler{}}
}

func (r *Registry) Register(method string, handler MethodHandler) {
	if r == nil || handler == nil {
		return
	}
	method = strings.ToLower(strings.TrimSpace(method))
	if method == "" {
		return
	}
	r.methods[method] = handler
}

func (r *Registry) Handle(ctx context.Context, req Request) (interface{}, *Error) {
	if r == nil {
		return nil, &Error{Code: "internal", Message: "rpc registry unavailable"}
	}
	handler := r.methods[req.NormalizedMethod()]
	if handler == nil {
		return nil, &Error{
			Code:    "invalid_argument",
			Message: "unknown method",
			Details: map[string]interface{}{"method": strings.TrimSpace(req.Method)},
		}
	}
	return handler(ctx, req.Params)
}

func RegisterJSON[T any](r *Registry, method string, handler func(context.Context, T) (interface{}, *Error)) {
	if r == nil || handler == nil {
		return
	}
	r.Register(method, func(ctx context.Context, raw json.RawMessage) (interface{}, *Error) {
		var params T
		if err := DecodeParams(raw, &params); err != nil {
			return nil, &Error{Code: "invalid_argument", Message: err.Error()}
		}
		return handler(ctx, params)
	})
}
