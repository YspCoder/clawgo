package tools

import "errors"

var (
	ErrUnsupportedAction = errors.New("unsupported action")
	ErrMissingField      = errors.New("missing required field")
)
