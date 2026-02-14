package tools

import (
	"context"
	"fmt"
	"clawgo/pkg/bus"
)

type SendCallback func(channel, chatID, content string, buttons [][]bus.Button) error

type MessageTool struct {
	sendCallback   SendCallback
	defaultChannel string
	defaultChatID  string
}

func NewMessageTool() *MessageTool {
	return &MessageTool{}
}

func (t *MessageTool) Name() string {
	return "message"
}

func (t *MessageTool) Description() string {
	return "Send a message to user on a chat channel. Use this when you want to communicate something."
}

func (t *MessageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The message content to send",
			},
			"channel": map[string]interface{}{
				"type":        "string",
				"description": "Optional: target channel (telegram, whatsapp, etc.)",
			},
			"chat_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional: target chat/user ID",
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
		"required": []string{"content"},
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
	content, ok := args["content"].(string)
	if !ok {
		return "", fmt.Errorf("content is required")
	}

	channel, _ := args["channel"].(string)
	chatID, _ := args["chat_id"].(string)

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
				var buttonRow []bus.Button
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
					buttons = append(buttons, buttonRow)
				}
			}
		}
	}

	if err := t.sendCallback(channel, chatID, content, buttons); err != nil {
		return fmt.Sprintf("Error sending message: %v", err), nil
	}

	return fmt.Sprintf("Message sent to %s:%s", channel, chatID), nil
}
