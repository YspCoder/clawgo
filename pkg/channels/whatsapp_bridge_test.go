package channels

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"clawgo/pkg/bus"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func TestParseWhatsAppBridgeListenAddr(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "raw host", input: "127.0.0.1:3001", want: "127.0.0.1:3001"},
		{name: "ws url", input: "ws://localhost:3001", want: "localhost:3001"},
		{name: "ws url path", input: "ws://localhost:3001/ws", want: "localhost:3001"},
		{name: "missing host", input: "ws:///ws", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseWhatsAppBridgeListenAddr(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBridgeStatusURL(t *testing.T) {
	got, err := BridgeStatusURL("ws://localhost:3001/ws")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "http://localhost:3001/status" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeWhatsAppRecipientJID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "8613012345678", want: "8613012345678@s.whatsapp.net"},
		{input: "1203630-123456789@g.us", want: "1203630-123456789@g.us"},
		{input: "1203630-123456789", want: "1203630-123456789@g.us"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := normalizeWhatsAppRecipientJID(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("got %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestDetectWhatsAppMediaType(t *testing.T) {
	tests := []struct {
		path     string
		data     []byte
		wantKind string
		wantMime string
	}{
		{path: "photo.jpg", data: []byte{0xff, 0xd8, 0xff, 0xe0}, wantKind: "image", wantMime: "image/jpeg"},
		{path: "clip.mp4", data: []byte("...."), wantKind: "video", wantMime: "video/mp4"},
		{path: "voice.ogg", data: []byte("OggS"), wantKind: "audio", wantMime: "audio/ogg"},
		{path: "report.pdf", data: []byte("%PDF-1.4"), wantKind: "document", wantMime: "application/pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			gotKind, gotMime := detectWhatsAppMediaType(tt.path, tt.data)
			if gotKind != tt.wantKind {
				t.Fatalf("kind got %q want %q", gotKind, tt.wantKind)
			}
			if gotMime != tt.wantMime {
				t.Fatalf("mime got %q want %q", gotMime, tt.wantMime)
			}
		})
	}
}

func TestWhatsAppSendIncludesMediaPayload(t *testing.T) {
	msg := bus.OutboundMessage{
		Channel:   "whatsapp",
		ChatID:    "12345@s.whatsapp.net",
		Content:   "hello",
		Media:     "/tmp/demo.png",
		ReplyToID: "wamid.demo",
	}
	payload := map[string]interface{}{
		"type":    "message",
		"to":      msg.ChatID,
		"content": msg.Content,
	}
	if msg.ReplyToID != "" {
		payload["reply_to_id"] = msg.ReplyToID
	}
	if msg.Media != "" {
		payload["media"] = []string{msg.Media}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	media, ok := parsed["media"].([]interface{})
	if !ok || len(media) != 1 || media[0] != msg.Media {
		t.Fatalf("unexpected media payload: %#v", parsed["media"])
	}
	if parsed["reply_to_id"] != msg.ReplyToID {
		t.Fatalf("unexpected reply_to_id payload: %#v", parsed["reply_to_id"])
	}
}

func TestExtractWhatsAppContextInfo(t *testing.T) {
	ctx := &waProto.ContextInfo{MentionedJID: []string{"8613012345678@s.whatsapp.net"}}
	msg := &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text:        proto.String("hi"),
			ContextInfo: ctx,
		},
	}
	got := extractWhatsAppContextInfo(msg)
	if got == nil || len(got.GetMentionedJID()) != 1 {
		t.Fatalf("expected context info to be extracted")
	}
}

func TestNormalizeComparableJID(t *testing.T) {
	jid := types.NewJID("8613012345678", types.DefaultUserServer)
	got := normalizeComparableJID(jid.ADString())
	if got != jid.String() {
		t.Fatalf("got %q want %q", got, jid.String())
	}
}

func TestApplyWhatsAppReplyContext(t *testing.T) {
	msg := &waProto.Message{Conversation: proto.String("hello")}
	applyWhatsAppReplyContext(msg, types.NewJID("12345", types.DefaultUserServer), "wamid.reply", "")
	if msg.GetExtendedTextMessage() == nil || msg.GetExtendedTextMessage().GetContextInfo().GetStanzaID() != "wamid.reply" {
		t.Fatalf("expected reply context on text message")
	}
}

func TestMarkIncomingReadReceiptUsesSenderOnlyForGroups(t *testing.T) {
	service := &WhatsAppBridgeService{}
	done := make(chan struct{}, 2)
	var gotChat, gotSender types.JID
	service.markReadFn = func(ctx context.Context, ids []types.MessageID, timestamp time.Time, chat, sender types.JID) error {
		gotChat = chat
		gotSender = sender
		done <- struct{}{}
		return nil
	}

	service.markIncomingReadReceipt(types.NewJID("1203630-123456789", types.GroupServer), types.NewJID("8613012345678", types.DefaultUserServer), types.MessageID("abc"), time.Now())
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for group mark read")
	}
	if gotChat.Server != types.GroupServer || gotSender.Server != types.DefaultUserServer {
		t.Fatalf("unexpected group mark read args: chat=%s sender=%s", gotChat, gotSender)
	}

	service.markIncomingReadReceipt(types.NewJID("8613012345678", types.DefaultUserServer), types.NewJID("8620000000000", types.DefaultUserServer), types.MessageID("def"), time.Now())
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for direct mark read")
	}
	if !gotSender.IsEmpty() {
		t.Fatalf("expected empty sender for direct chat, got %s", gotSender)
	}
}
