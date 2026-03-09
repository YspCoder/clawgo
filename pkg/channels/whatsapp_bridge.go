package channels

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"
)

type WhatsAppBridgeStatus struct {
	State            string `json:"state"`
	Connected        bool   `json:"connected"`
	LoggedIn         bool   `json:"logged_in"`
	BridgeAddr       string `json:"bridge_addr"`
	UserJID          string `json:"user_jid,omitempty"`
	PushName         string `json:"push_name,omitempty"`
	Platform         string `json:"platform,omitempty"`
	QRCode           string `json:"qr_code,omitempty"`
	QRAvailable      bool   `json:"qr_available"`
	LastEvent        string `json:"last_event,omitempty"`
	LastError        string `json:"last_error,omitempty"`
	UpdatedAt        string `json:"updated_at"`
	InboundCount     int    `json:"inbound_count"`
	OutboundCount    int    `json:"outbound_count"`
	ReadReceiptCount int    `json:"read_receipt_count"`
	LastInboundAt    string `json:"last_inbound_at,omitempty"`
	LastOutboundAt   string `json:"last_outbound_at,omitempty"`
	LastReadAt       string `json:"last_read_at,omitempty"`
	LastInboundFrom  string `json:"last_inbound_from,omitempty"`
	LastOutboundTo   string `json:"last_outbound_to,omitempty"`
	LastInboundText  string `json:"last_inbound_text,omitempty"`
	LastOutboundText string `json:"last_outbound_text,omitempty"`
}

type WhatsAppBridgeService struct {
	addr        string
	stateDir    string
	printQR     bool
	httpServer  *http.Server
	client      *whatsmeow.Client
	container   *sqlstore.Container
	rawDB       *sql.DB
	cancel      context.CancelFunc
	wsUpgrader  websocket.Upgrader
	wsClients   map[*websocket.Conn]struct{}
	statusMu    sync.RWMutex
	status      WhatsAppBridgeStatus
	wsClientsMu sync.Mutex
	markReadFn  func(ctx context.Context, ids []types.MessageID, timestamp time.Time, chat, sender types.JID) error
}

type whatsappBridgeWSMessage struct {
	Type          string   `json:"type"`
	To            string   `json:"to,omitempty"`
	From          string   `json:"from,omitempty"`
	Chat          string   `json:"chat,omitempty"`
	Content       string   `json:"content,omitempty"`
	ReplyToID     string   `json:"reply_to_id,omitempty"`
	ReplyToSender string   `json:"reply_to_sender,omitempty"`
	ID            string   `json:"id,omitempty"`
	FromName      string   `json:"from_name,omitempty"`
	Media         []string `json:"media,omitempty"`
}

func NewWhatsAppBridgeService(addr, stateDir string, printQR bool) *WhatsAppBridgeService {
	return &WhatsAppBridgeService{
		addr:     strings.TrimSpace(addr),
		stateDir: strings.TrimSpace(stateDir),
		printQR:  printQR,
		wsUpgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		wsClients: map[*websocket.Conn]struct{}{},
		status: WhatsAppBridgeStatus{
			State:      "starting",
			BridgeAddr: strings.TrimSpace(addr),
			UpdatedAt:  time.Now().Format(time.RFC3339),
		},
	}
}

func (s *WhatsAppBridgeService) Start(ctx context.Context) error {
	if strings.TrimSpace(s.addr) == "" {
		return fmt.Errorf("bridge address is required")
	}
	if strings.TrimSpace(s.stateDir) == "" {
		return fmt.Errorf("bridge state directory is required")
	}
	if err := os.MkdirAll(s.stateDir, 0o755); err != nil {
		return fmt.Errorf("create whatsapp state dir: %w", err)
	}
	if err := s.initClient(ctx); err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWS)
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/logout", s.handleLogout)
	s.httpServer = &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen whatsapp bridge: %w", err)
	}

	go func() {
		<-runCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
		s.closeWSClients()
		if s.client != nil {
			s.client.Disconnect()
		}
		if s.rawDB != nil {
			_ = s.rawDB.Close()
		}
	}()

	go func() {
		_ = s.connectClient(runCtx)
	}()

	if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *WhatsAppBridgeService) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *WhatsAppBridgeService) StatusSnapshot() WhatsAppBridgeStatus {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	return s.status
}

func (s *WhatsAppBridgeService) initClient(ctx context.Context) error {
	dbPath := filepath.Join(s.stateDir, "whatsmeow.sqlite")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open whatsapp sqlite store: %w", err)
	}
	if _, err := rawDB.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = rawDB.Close()
		return fmt.Errorf("enable whatsapp sqlite foreign keys: %w", err)
	}
	container := sqlstore.NewWithDB(rawDB, "sqlite", waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		_ = rawDB.Close()
		return fmt.Errorf("upgrade whatsapp sqlite store: %w", err)
	}
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		_ = rawDB.Close()
		return fmt.Errorf("load whatsapp device store: %w", err)
	}
	client := whatsmeow.NewClient(deviceStore, waLog.Noop)
	client.EnableAutoReconnect = true
	client.AddEventHandler(s.handleWAEvent)

	s.rawDB = rawDB
	s.container = container
	s.client = client
	s.markReadFn = func(ctx context.Context, ids []types.MessageID, timestamp time.Time, chat, sender types.JID) error {
		return client.MarkRead(ctx, ids, timestamp, chat, sender)
	}
	if deviceStore.ID != nil {
		s.updateStatus(func(st *WhatsAppBridgeStatus) {
			st.LoggedIn = true
			st.UserJID = deviceStore.ID.String()
			st.State = "stored_session"
			st.LastEvent = "stored_session"
		})
	}
	return nil
}

func (s *WhatsAppBridgeService) connectClient(ctx context.Context) error {
	if s.client == nil {
		return fmt.Errorf("whatsapp bridge client is not initialized")
	}

	var qrChan <-chan whatsmeow.QRChannelItem
	var err error
	if s.client.Store.ID == nil {
		qrChan, err = s.client.GetQRChannel(ctx)
		if err != nil && !errors.Is(err, whatsmeow.ErrQRStoreContainsID) {
			s.updateStatus(func(st *WhatsAppBridgeStatus) {
				st.State = "error"
				st.LastError = err.Error()
				st.LastEvent = "qr_init_failed"
			})
			return err
		}
		if qrChan != nil {
			go s.consumeQRChannel(ctx, qrChan)
		}
	}

	if err := s.client.Connect(); err != nil {
		s.updateStatus(func(st *WhatsAppBridgeStatus) {
			st.State = "error"
			st.Connected = false
			st.LastError = err.Error()
			st.LastEvent = "connect_failed"
		})
		return fmt.Errorf("connect whatsapp bridge: %w", err)
	}
	return nil
}

func (s *WhatsAppBridgeService) consumeQRChannel(ctx context.Context, qrChan <-chan whatsmeow.QRChannelItem) {
	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-qrChan:
			if !ok {
				return
			}
			switch item.Event {
			case "code":
				s.updateStatus(func(st *WhatsAppBridgeStatus) {
					st.State = "qr_ready"
					st.QRCode = item.Code
					st.QRAvailable = item.Code != ""
					st.LastEvent = "qr_ready"
				})
			default:
				s.updateStatus(func(st *WhatsAppBridgeStatus) {
					st.LastEvent = item.Event
					if item.Event == whatsmeow.QRChannelSuccess.Event {
						st.State = "paired"
						st.QRCode = ""
						st.QRAvailable = false
					}
				})
			}
		}
	}
}

func (s *WhatsAppBridgeService) handleWAEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Connected:
		s.updateStatus(func(st *WhatsAppBridgeStatus) {
			st.State = "connected"
			st.Connected = true
			st.LoggedIn = s.client != nil && s.client.Store.ID != nil
			st.QRCode = ""
			st.QRAvailable = false
			st.LastEvent = "connected"
			if s.client != nil && s.client.Store.ID != nil {
				st.UserJID = s.client.Store.ID.String()
			}
		})
	case *events.Disconnected:
		s.updateStatus(func(st *WhatsAppBridgeStatus) {
			st.Connected = false
			if st.LoggedIn {
				st.State = "disconnected"
			} else {
				st.State = "waiting_qr"
			}
			st.LastEvent = "disconnected"
		})
	case *events.PairSuccess:
		s.updateStatus(func(st *WhatsAppBridgeStatus) {
			st.State = "paired"
			st.LoggedIn = true
			st.UserJID = v.ID.String()
			st.Platform = v.Platform
			st.QRCode = ""
			st.QRAvailable = false
			st.LastEvent = "pair_success"
		})
	case *events.LoggedOut:
		s.updateStatus(func(st *WhatsAppBridgeStatus) {
			st.State = "logged_out"
			st.Connected = false
			st.LoggedIn = false
			st.UserJID = ""
			st.QRCode = ""
			st.QRAvailable = false
			st.LastEvent = "logged_out"
		})
	case *events.StreamReplaced:
		s.updateStatus(func(st *WhatsAppBridgeStatus) {
			st.State = "stream_replaced"
			st.Connected = false
			st.LastEvent = "stream_replaced"
		})
	case *events.ClientOutdated:
		s.updateStatus(func(st *WhatsAppBridgeStatus) {
			st.State = "client_outdated"
			st.Connected = false
			st.LastError = "whatsapp web client outdated"
			st.LastEvent = "client_outdated"
		})
	case *events.ConnectFailure:
		s.updateStatus(func(st *WhatsAppBridgeStatus) {
			st.State = "connect_failed"
			st.Connected = false
			st.LastError = v.Reason.String()
			st.LastEvent = "connect_failure"
		})
	case *events.TemporaryBan:
		s.updateStatus(func(st *WhatsAppBridgeStatus) {
			st.State = "temporary_ban"
			st.Connected = false
			st.LastError = v.String()
			st.LastEvent = "temporary_ban"
		})
	case *events.Message:
		if v.Info.IsFromMe {
			return
		}
		isGroup := v.Info.Chat.Server == types.GroupServer
		mentionedSelf, replyToMe := s.matchCurrentUserContext(v.Message)
		payload := whatsappBridgeWSMessage{
			Type:     "message",
			From:     v.Info.Sender.ToNonAD().String(),
			Chat:     v.Info.Chat.ToNonAD().String(),
			Content:  extractWhatsAppMessageText(v.Message),
			ID:       v.Info.ID,
			FromName: v.Info.PushName,
		}
		s.broadcastWSMap(map[string]interface{}{
			"type":           payload.Type,
			"from":           payload.From,
			"chat":           payload.Chat,
			"content":        payload.Content,
			"id":             payload.ID,
			"from_name":      payload.FromName,
			"is_group":       isGroup,
			"mentioned_self": mentionedSelf,
			"reply_to_me":    replyToMe,
		})
		s.updateStatus(func(st *WhatsAppBridgeStatus) {
			st.InboundCount++
			st.LastInboundAt = time.Now().Format(time.RFC3339)
			st.LastInboundFrom = payload.From
			st.LastInboundText = truncateString(strings.TrimSpace(payload.Content), 120)
			st.LastEvent = "message_inbound"
		})
		s.markIncomingReadReceipt(v.Info.Chat.ToNonAD(), v.Info.Sender.ToNonAD(), v.Info.ID, v.Info.Timestamp)
	}
}

func (s *WhatsAppBridgeService) handleWS(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message": "whatsapp bridge websocket endpoint",
		})
		return
	}
	conn, err := s.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.wsClientsMu.Lock()
	s.wsClients[conn] = struct{}{}
	s.wsClientsMu.Unlock()
	defer func() {
		s.wsClientsMu.Lock()
		delete(s.wsClients, conn)
		s.wsClientsMu.Unlock()
		_ = conn.Close()
	}()

	for {
		var msg whatsappBridgeWSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		if strings.TrimSpace(msg.Type) != "message" {
			continue
		}
		if err := s.sendOutboundMessage(r.Context(), msg.To, msg.Content, msg.Media, msg.ReplyToID, msg.ReplyToSender); err != nil {
			_ = conn.WriteJSON(map[string]string{
				"type":  "error",
				"error": err.Error(),
			})
			continue
		}
	}
}

func (s *WhatsAppBridgeService) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.StatusSnapshot())
}

func (s *WhatsAppBridgeService) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.client == nil {
		http.Error(w, "whatsapp bridge client is not initialized", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := s.client.Logout(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.updateStatus(func(st *WhatsAppBridgeStatus) {
		st.State = "logged_out"
		st.Connected = false
		st.LoggedIn = false
		st.UserJID = ""
		st.QRCode = ""
		st.QRAvailable = false
		st.LastEvent = "logout"
	})
	go func() {
		_ = s.connectClient(context.Background())
	}()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.StatusSnapshot())
}

func (s *WhatsAppBridgeService) sendTextMessage(ctx context.Context, rawTo, content, replyToID, replyToSender string) error {
	if s.client == nil {
		return fmt.Errorf("whatsapp client not initialized")
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("message content is required")
	}
	to, err := normalizeWhatsAppRecipientJID(rawTo)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(content)
	msg := &waProto.Message{
		Conversation: &text,
	}
	applyWhatsAppReplyContext(msg, to, strings.TrimSpace(replyToID), strings.TrimSpace(replyToSender))
	_, err = s.client.SendMessage(ctx, to, msg)
	if err == nil {
		s.updateStatus(func(st *WhatsAppBridgeStatus) {
			st.OutboundCount++
			st.LastOutboundAt = time.Now().Format(time.RFC3339)
			st.LastOutboundTo = to.String()
			st.LastOutboundText = truncateString(text, 120)
			st.LastEvent = "message_outbound"
		})
	}
	return err
}

func (s *WhatsAppBridgeService) sendOutboundMessage(ctx context.Context, rawTo, content string, mediaPaths []string, replyToID, replyToSender string) error {
	if len(mediaPaths) == 0 {
		return s.sendTextMessage(ctx, rawTo, content, replyToID, replyToSender)
	}
	to, err := normalizeWhatsAppRecipientJID(rawTo)
	if err != nil {
		return err
	}
	caption := strings.TrimSpace(content)
	for idx, mediaPath := range mediaPaths {
		msg, err := s.buildMediaMessage(ctx, to, strings.TrimSpace(mediaPath), caption, replyToID, replyToSender)
		if err != nil {
			return err
		}
		if _, err := s.client.SendMessage(ctx, to, msg); err != nil {
			return err
		}
		s.updateStatus(func(st *WhatsAppBridgeStatus) {
			st.OutboundCount++
			st.LastOutboundAt = time.Now().Format(time.RFC3339)
			st.LastOutboundTo = to.String()
			st.LastOutboundText = truncateString(strings.TrimSpace(content), 120)
			st.LastEvent = "message_outbound"
		})
		if idx == 0 {
			caption = ""
		}
	}
	return nil
}

func (s *WhatsAppBridgeService) buildMediaMessage(ctx context.Context, to types.JID, mediaPath, caption, replyToID, replyToSender string) (*waProto.Message, error) {
	if s.client == nil {
		return nil, fmt.Errorf("whatsapp client not initialized")
	}
	mediaPath = strings.TrimSpace(mediaPath)
	if mediaPath == "" {
		return nil, fmt.Errorf("media path is required")
	}
	data, err := os.ReadFile(mediaPath)
	if err != nil {
		return nil, fmt.Errorf("read media file: %w", err)
	}
	kind, mimeType := detectWhatsAppMediaType(mediaPath, data)
	uploadType := whatsmeow.MediaDocument
	switch kind {
	case "image":
		uploadType = whatsmeow.MediaImage
	case "video":
		uploadType = whatsmeow.MediaVideo
	case "audio":
		uploadType = whatsmeow.MediaAudio
	}
	resp, err := s.client.Upload(ctx, data, uploadType)
	if err != nil {
		return nil, fmt.Errorf("upload media: %w", err)
	}
	fileLength := resp.FileLength
	fileName := filepath.Base(mediaPath)
	switch kind {
	case "image":
		msg := &waProto.Message{
			ImageMessage: &waProto.ImageMessage{
				Caption:       proto.String(strings.TrimSpace(caption)),
				Mimetype:      proto.String(mimeType),
				URL:           proto.String(resp.URL),
				DirectPath:    proto.String(resp.DirectPath),
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    proto.Uint64(fileLength),
			},
		}
		applyWhatsAppReplyContext(msg, to, strings.TrimSpace(replyToID), strings.TrimSpace(replyToSender))
		return msg, nil
	case "video":
		msg := &waProto.Message{
			VideoMessage: &waProto.VideoMessage{
				Caption:       proto.String(strings.TrimSpace(caption)),
				Mimetype:      proto.String(mimeType),
				URL:           proto.String(resp.URL),
				DirectPath:    proto.String(resp.DirectPath),
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    proto.Uint64(fileLength),
			},
		}
		applyWhatsAppReplyContext(msg, to, strings.TrimSpace(replyToID), strings.TrimSpace(replyToSender))
		return msg, nil
	case "audio":
		msg := &waProto.Message{
			AudioMessage: &waProto.AudioMessage{
				Mimetype:      proto.String(mimeType),
				URL:           proto.String(resp.URL),
				DirectPath:    proto.String(resp.DirectPath),
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    proto.Uint64(fileLength),
			},
		}
		applyWhatsAppReplyContext(msg, to, strings.TrimSpace(replyToID), strings.TrimSpace(replyToSender))
		return msg, nil
	default:
		msg := &waProto.Message{
			DocumentMessage: &waProto.DocumentMessage{
				Caption:       proto.String(strings.TrimSpace(caption)),
				Mimetype:      proto.String(mimeType),
				Title:         proto.String(fileName),
				FileName:      proto.String(fileName),
				URL:           proto.String(resp.URL),
				DirectPath:    proto.String(resp.DirectPath),
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    proto.Uint64(fileLength),
			},
		}
		applyWhatsAppReplyContext(msg, to, strings.TrimSpace(replyToID), strings.TrimSpace(replyToSender))
		return msg, nil
	}
}

func detectWhatsAppMediaType(path string, data []byte) (kind string, mimeType string) {
	ext := strings.ToLower(filepath.Ext(path))
	mimeType = mime.TypeByExtension(ext)
	if mimeType == "" && len(data) > 0 {
		mimeType = http.DetectContentType(data)
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image", mimeType
	case strings.HasPrefix(mimeType, "video/"):
		return "video", mimeType
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio", mimeType
	default:
		return "document", mimeType
	}
}

func (s *WhatsAppBridgeService) matchCurrentUserContext(msg *waProto.Message) (mentionedSelf bool, replyToMe bool) {
	if s.client == nil || s.client.Store.ID == nil || msg == nil {
		return false, false
	}
	ctx := extractWhatsAppContextInfo(msg)
	if ctx == nil {
		return false, false
	}
	own := s.client.Store.ID.ToNonAD().String()
	for _, mentioned := range ctx.GetMentionedJID() {
		if normalizeComparableJID(mentioned) == own {
			mentionedSelf = true
			break
		}
	}
	replyParticipant := normalizeComparableJID(ctx.GetParticipant())
	if replyParticipant != "" && replyParticipant == own {
		replyToMe = true
	}
	return mentionedSelf, replyToMe
}

func extractWhatsAppContextInfo(msg *waProto.Message) *waProto.ContextInfo {
	switch {
	case msg == nil:
		return nil
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetContextInfo()
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetContextInfo()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetContextInfo()
	case msg.GetAudioMessage() != nil:
		return msg.GetAudioMessage().GetContextInfo()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetContextInfo()
	case msg.GetDocumentWithCaptionMessage() != nil && msg.GetDocumentWithCaptionMessage().GetMessage() != nil:
		return extractWhatsAppContextInfo(msg.GetDocumentWithCaptionMessage().GetMessage())
	default:
		return nil
	}
}

func normalizeComparableJID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	jid, err := types.ParseJID(raw)
	if err == nil {
		return jid.ToNonAD().String()
	}
	return raw
}

func applyWhatsAppReplyContext(msg *waProto.Message, chatJID types.JID, replyToID, replyToSender string) {
	if msg == nil || strings.TrimSpace(replyToID) == "" {
		return
	}
	ctx := &waProto.ContextInfo{
		StanzaID: proto.String(strings.TrimSpace(replyToID)),
	}
	if chatJID.Server == types.GroupServer {
		ctx.RemoteJID = proto.String(chatJID.ToNonAD().String())
		if sender := normalizeComparableJID(replyToSender); sender != "" {
			ctx.Participant = proto.String(sender)
		}
	}
	switch {
	case msg.GetExtendedTextMessage() != nil:
		msg.GetExtendedTextMessage().ContextInfo = ctx
	case msg.GetImageMessage() != nil:
		msg.GetImageMessage().ContextInfo = ctx
	case msg.GetVideoMessage() != nil:
		msg.GetVideoMessage().ContextInfo = ctx
	case msg.GetAudioMessage() != nil:
		msg.GetAudioMessage().ContextInfo = ctx
	case msg.GetDocumentMessage() != nil:
		msg.GetDocumentMessage().ContextInfo = ctx
	default:
		msg.ExtendedTextMessage = &waProto.ExtendedTextMessage{
			Text:        proto.String(msg.GetConversation()),
			ContextInfo: ctx,
		}
		msg.Conversation = nil
	}
}

func (s *WhatsAppBridgeService) markIncomingReadReceipt(chat, sender types.JID, id types.MessageID, timestamp time.Time) {
	if s == nil || s.markReadFn == nil || id == "" || chat.IsEmpty() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		effectiveSender := types.EmptyJID
		if chat.Server == types.GroupServer {
			effectiveSender = sender
		}
		if err := s.markReadFn(ctx, []types.MessageID{id}, timestamp, chat, effectiveSender); err != nil {
			s.updateStatus(func(st *WhatsAppBridgeStatus) {
				st.LastError = "mark_read_failed: " + err.Error()
				st.LastEvent = "mark_read_failed"
			})
			return
		}
		s.updateStatus(func(st *WhatsAppBridgeStatus) {
			st.ReadReceiptCount++
			st.LastReadAt = time.Now().Format(time.RFC3339)
			st.LastEvent = "mark_read"
		})
	}()
}

func (s *WhatsAppBridgeService) updateStatus(mut func(*WhatsAppBridgeStatus)) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	mut(&s.status)
	s.status.UpdatedAt = time.Now().Format(time.RFC3339)
}

func (s *WhatsAppBridgeService) broadcastWS(payload whatsappBridgeWSMessage) {
	s.wsClientsMu.Lock()
	defer s.wsClientsMu.Unlock()
	for conn := range s.wsClients {
		_ = conn.WriteJSON(payload)
	}
}

func (s *WhatsAppBridgeService) broadcastWSMap(payload map[string]interface{}) {
	s.wsClientsMu.Lock()
	defer s.wsClientsMu.Unlock()
	for conn := range s.wsClients {
		_ = conn.WriteJSON(payload)
	}
}

func (s *WhatsAppBridgeService) closeWSClients() {
	s.wsClientsMu.Lock()
	defer s.wsClientsMu.Unlock()
	for conn := range s.wsClients {
		_ = conn.Close()
		delete(s.wsClients, conn)
	}
}

func ParseWhatsAppBridgeListenAddr(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("bridge url is required")
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("parse bridge url: %w", err)
		}
		if strings.TrimSpace(u.Host) == "" {
			return "", fmt.Errorf("bridge url host is required")
		}
		return u.Host, nil
	}
	return raw, nil
}

func BridgeStatusURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("bridge url is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "ws://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse bridge url: %w", err)
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	default:
		u.Scheme = "http"
	}
	u.Path = "/status"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func BridgeLogoutURL(raw string) (string, error) {
	statusURL, err := BridgeStatusURL(raw)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(statusURL, "/status") + "/logout", nil
}

func normalizeWhatsAppRecipientJID(raw string) (types.JID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return types.EmptyJID, fmt.Errorf("recipient is required")
	}
	if strings.Contains(raw, "@") {
		jid, err := types.ParseJID(raw)
		if err != nil {
			return types.EmptyJID, fmt.Errorf("parse recipient jid: %w", err)
		}
		return jid.ToNonAD(), nil
	}
	if strings.Contains(raw, "-") {
		return types.NewJID(raw, types.GroupServer), nil
	}
	return types.NewJID(raw, types.DefaultUserServer), nil
}

func extractWhatsAppMessageText(msg *waProto.Message) string {
	if msg == nil {
		return ""
	}
	switch {
	case strings.TrimSpace(msg.GetConversation()) != "":
		return msg.GetConversation()
	case msg.GetExtendedTextMessage() != nil && strings.TrimSpace(msg.GetExtendedTextMessage().GetText()) != "":
		return msg.GetExtendedTextMessage().GetText()
	case msg.GetImageMessage() != nil && strings.TrimSpace(msg.GetImageMessage().GetCaption()) != "":
		return msg.GetImageMessage().GetCaption()
	case msg.GetVideoMessage() != nil && strings.TrimSpace(msg.GetVideoMessage().GetCaption()) != "":
		return msg.GetVideoMessage().GetCaption()
	case msg.GetDocumentMessage() != nil && strings.TrimSpace(msg.GetDocumentMessage().GetCaption()) != "":
		return msg.GetDocumentMessage().GetCaption()
	case msg.GetAudioMessage() != nil:
		return "[audio]"
	case msg.GetStickerMessage() != nil:
		return "[sticker]"
	case msg.GetImageMessage() != nil:
		return "[image]"
	case msg.GetVideoMessage() != nil:
		return "[video]"
	case msg.GetDocumentMessage() != nil:
		return "[document]"
	default:
		return ""
	}
}
