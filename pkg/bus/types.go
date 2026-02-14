package bus

type InboundMessage struct {
	Channel    string            `json:"channel"`
	SenderID   string            `json:"sender_id"`
	ChatID     string            `json:"chat_id"`
	Content    string            `json:"content"`
	Media      []string          `json:"media,omitempty"`
	SessionKey string            `json:"session_key"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type Button struct {
	Text string `json:"text"`
	Data string `json:"data"`
}

type OutboundMessage struct {
	Channel string     `json:"channel"`
	ChatID  string     `json:"chat_id"`
	Content string     `json:"content"`
	Buttons [][]Button `json:"buttons,omitempty"`
}

type MessageHandler func(InboundMessage) error
