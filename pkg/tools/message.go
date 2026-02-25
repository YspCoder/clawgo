package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"clawgo/pkg/bus"
)

type SendCallback func(channel, chatID, action, content, media, messageID, emoji string, buttons [][]bus.Button) error

type MessageTool struct {
	sendCallback   SendCallback
	defaultChannel string
	defaultChatID  string
}

var buttonRowPool = sync.Pool{New: func() interface{} { return make([]bus.Button, 0, 8) }}

func NewMessageTool() *MessageTool {
	return &MessageTool{}
}

func (t *MessageTool) Name() string {
	return "message"
}

func (t *MessageTool) Description() string {
	return "Channel actions tool. Supports action=send (default). OpenClaw-compatible fields: action, message/content, to/chat_id, channel."
}

func (t *MessageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action type: send|edit|delete|react",
			},
			"message": map[string]interface{}{
				"type":        "string",
				"description": "The message content to send (OpenClaw-compatible)",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Alias of message",
			},
			"to": map[string]interface{}{
				"type":        "string",
				"description": "Optional target id (alias of chat_id)",
			},
			"channel": map[string]interface{}{
				"type":        "string",
				"description": "Optional: target channel (telegram, whatsapp, etc.)",
			},
			"chat_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional: target chat/user ID",
			},
			"media": map[string]interface{}{
				"type":        "string",
				"description": "Optional media path or URL for action=send",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Alias of media",
			},
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "Alias of media",
			},
			"filePath": map[string]interface{}{
				"type":        "string",
				"description": "Alias of media",
			},
			"message_id": map[string]interface{}{
				"type":        "string",
				"description": "Target message id for edit/delete/react",
			},
			"emoji": map[string]interface{}{
				"type":        "string",
				"description": "Emoji for react action",
			},
			"buttons": map[string]interface{}{
				"type":        "array",
				"description": "Optional: buttons to include in the message (2D array: rows of buttons)",
				"items": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"text": map[string]interface{}{"type": "string", "description": "Button text"},
							"data": map[string]interface{}{"type": "string", "description": "Callback data"},
						},
						"required": []string{"text", "data"},
					},
				},
			},
		},
		"required": []string{},
	}
}

func (t *MessageTool) SetContext(channel, chatID string) {
	t.defaultChannel = channel
	t.defaultChatID = chatID
}

func (t *MessageTool) SetSendCallback(callback SendCallback) {
	t.sendCallback = callback
}

func (t *MessageTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	action, _ := args["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		action = "send"
	}
	content, _ := args["content"].(string)
	if msg, _ := args["message"].(string); msg != "" {
		content = msg
	}
	media, _ := args["media"].(string)
	if media == "" {
		if p, _ := args["path"].(string); p != "" {
			media = p
		}
	}
	if media == "" {
		if p, _ := args["file_path"].(string); p != "" {
			media = p
		}
	}
	if media == "" {
		if p, _ := args["filePath"].(string); p != "" {
			media = p
		}
	}
	messageID, _ := args["message_id"].(string)
	emoji, _ := args["emoji"].(string)

	switch action {
	case "send":
		if content == "" && media == "" {
			return "", fmt.Errorf("%w: message/content or media for action=send", ErrMissingField)
		}
	case "edit":
		if messageID == "" || content == "" {
			return "", fmt.Errorf("%w: message_id and message/content for action=edit", ErrMissingField)
		}
	case "delete":
		if messageID == "" {
			return "", fmt.Errorf("%w: message_id for action=delete", ErrMissingField)
		}
	case "react":
		if messageID == "" || emoji == "" {
			return "", fmt.Errorf("%w: message_id and emoji for action=react", ErrMissingField)
		}
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedAction, action)
	}

	channel, _ := args["channel"].(string)
	chatID, _ := args["chat_id"].(string)
	if to, _ := args["to"].(string); to != "" {
		chatID = to
	}

	if channel == "" {
		channel = t.defaultChannel
	}
	if chatID == "" {
		chatID = t.defaultChatID
	}

	if channel == "" || chatID == "" {
		return "Error: No target channel/chat specified", nil
	}

	if t.sendCallback == nil {
		return "Error: Message sending not configured", nil
	}

	var buttons [][]bus.Button
	if btns, ok := args["buttons"].([]interface{}); ok {
		for _, row := range btns {
			if rowArr, ok := row.([]interface{}); ok {
				pooled := buttonRowPool.Get().([]bus.Button)
				buttonRow := pooled[:0]
				for _, b := range rowArr {
					if bMap, ok := b.(map[string]interface{}); ok {
						text, _ := bMap["text"].(string)
						data, _ := bMap["data"].(string)
						if text != "" && data != "" {
							buttonRow = append(buttonRow, bus.Button{Text: text, Data: data})
						}
					}
				}
				if len(buttonRow) > 0 {
					copied := append([]bus.Button(nil), buttonRow...)
					buttons = append(buttons, copied)
				}
				buttonRowPool.Put(buttonRow[:0])
			}
		}
	}

	if err := t.sendCallback(channel, chatID, action, content, media, messageID, emoji, buttons); err != nil {
		return fmt.Sprintf("Error sending message: %v", err), nil
	}

	return fmt.Sprintf("Message action=%s sent to %s:%s", action, channel, chatID), nil
}
