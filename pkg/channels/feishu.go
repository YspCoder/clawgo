package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkauthv3 "github.com/larksuite/oapi-sdk-go/v3/service/auth/v3"
	larkdrivev2 "github.com/larksuite/oapi-sdk-go/v3/service/drive/v2"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larksheets "github.com/larksuite/oapi-sdk-go/v3/service/sheets/v3"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/logger"
)

type FeishuChannel struct {
	*BaseChannel
	config   config.FeishuConfig
	client   *lark.Client
	wsClient *larkws.Client

	mu        sync.Mutex
	runCancel cancelGuard

	tokenMu           sync.Mutex
	tenantTokenOnce   sync.Once
	tenantAccessToken string
	tenantTokenExpire time.Time
	tenantTokenErr    error
}

func (c *FeishuChannel) SupportsAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", "send":
		return true
	default:
		return false
	}
}

func NewFeishuChannel(cfg config.FeishuConfig, bus *bus.MessageBus) (*FeishuChannel, error) {
	base := NewBaseChannel("feishu", cfg, bus, cfg.AllowFrom)

	return &FeishuChannel{
		BaseChannel: base,
		config:      cfg,
		client:      lark.NewClient(cfg.AppID, cfg.AppSecret),
	}, nil
}

func (c *FeishuChannel) Start(ctx context.Context) error {
	if c.IsRunning() {
		return nil
	}
	if c.config.AppID == "" || c.config.AppSecret == "" {
		return fmt.Errorf("feishu app_id or app_secret is empty")
	}

	dispatcher := larkdispatcher.NewEventDispatcher(c.config.VerificationToken, c.config.EncryptKey).
		OnP2MessageReceiveV1(c.handleMessageReceive)

	runCtx, cancel := context.WithCancel(ctx)
	c.runCancel.set(cancel)

	c.mu.Lock()
	c.wsClient = larkws.NewClient(
		c.config.AppID,
		c.config.AppSecret,
		larkws.WithEventHandler(dispatcher),
	)
	wsClient := c.wsClient
	c.mu.Unlock()

	c.setRunning(true)
	logger.InfoC("feishu", logger.C0043)

	runChannelTask("feishu", "websocket", func() error {
		return wsClient.Start(runCtx)
	}, func(_ error) {
		c.setRunning(false)
	})

	return nil
}

func (c *FeishuChannel) Stop(ctx context.Context) error {
	if !c.IsRunning() {
		return nil
	}
	c.mu.Lock()
	c.wsClient = nil
	c.mu.Unlock()
	c.runCancel.cancelAndClear()

	c.setRunning(false)
	logger.InfoC("feishu", logger.C0044)
	return nil
}

func (c *FeishuChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("feishu channel not running")
	}

	if msg.ChatID == "" {
		return fmt.Errorf("chat ID is empty")
	}
	action := strings.ToLower(strings.TrimSpace(msg.Action))
	if action != "" && action != "send" {
		return fmt.Errorf("unsupported feishu action: %s", action)
	}

	workMsg := msg
	var tables []feishuTableData
	if strings.TrimSpace(workMsg.Media) == "" {
		workMsg.Content, tables = extractMarkdownTables(strings.TrimSpace(workMsg.Content))
		links := make([]string, 0, len(tables))
		for i, t := range tables {
			link, lerr := c.createFeishuSheetFromTable(ctx, t.Name, t.Rows)
			if lerr != nil {
				logger.WarnCF("feishu", logger.C0045, map[string]interface{}{logger.FieldError: lerr.Error(), logger.FieldChatID: msg.ChatID})
				continue
			}
			links = append(links, fmt.Sprintf("琛ㄦ牸%d: %s", i+1, link))
		}
		if len(links) > 0 {
			if strings.TrimSpace(workMsg.Content) != "" {
				workMsg.Content = strings.TrimSpace(workMsg.Content) + "\n\n"
			}
			workMsg.Content += strings.Join(links, "\n")
		}
	}

	msgType, contentPayload, err := buildFeishuOutbound(workMsg)
	if err != nil {
		return err
	}
	if strings.TrimSpace(workMsg.Media) != "" {
		msgType, contentPayload, err = c.buildFeishuMediaOutbound(ctx, strings.TrimSpace(workMsg.Media))
		if err != nil {
			return err
		}
	}

	if err := c.sendFeishuMessage(ctx, msg.ChatID, msgType, contentPayload); err != nil {
		return err
	}

	logger.InfoCF("feishu", logger.C0046, map[string]interface{}{
		logger.FieldChatID: msg.ChatID,
		"msg_type":         msgType,
		"has_media":        strings.TrimSpace(workMsg.Media) != "",
		"tables":           len(tables),
	})

	return nil
}

func (c *FeishuChannel) handleMessageReceive(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil
	}

	message := event.Event.Message
	sender := event.Event.Sender

	chatID := stringValue(message.ChatId)
	if chatID == "" {
		return nil
	}
	chatType := strings.ToLower(strings.TrimSpace(stringValue(message.ChatType)))
	if !c.isAllowedChat(chatID, chatType) {
		logger.WarnCF("feishu", logger.C0047, map[string]interface{}{
			logger.FieldSenderID: extractFeishuSenderID(sender),
			logger.FieldChatID:   chatID,
			"chat_type":          chatType,
		})
		return nil
	}

	senderID := extractFeishuSenderID(sender)
	if senderID == "" {
		senderID = "unknown"
	}

	content, mediaPaths := extractFeishuMessageContent(message)
	if content == "" {
		content = "[empty message]"
	}
	if !c.shouldHandleGroupMessage(chatType, content) {
		logger.DebugCF("feishu", logger.C0048, map[string]interface{}{
			logger.FieldSenderID: senderID,
			logger.FieldChatID:   chatID,
		})
		return nil
	}

	metadata := map[string]string{}
	if messageID := stringValue(message.MessageId); messageID != "" {
		metadata["message_id"] = messageID
	}
	if messageType := stringValue(message.MessageType); messageType != "" {
		metadata["message_type"] = messageType
	}
	if chatType := stringValue(message.ChatType); chatType != "" {
		metadata["chat_type"] = chatType
	}
	if sender != nil && sender.TenantKey != nil {
		metadata["tenant_key"] = *sender.TenantKey
	}

	logger.InfoCF("feishu", logger.C0049, map[string]interface{}{
		logger.FieldSenderID: senderID,
		logger.FieldChatID:   chatID,
		logger.FieldPreview:  truncateString(content, 80),
	})

	resolvedMedia := c.resolveInboundMedia(ctx, mediaPaths)
	c.HandleMessage(senderID, chatID, content, resolvedMedia, metadata)
	return nil
}

func (c *FeishuChannel) resolveInboundMedia(ctx context.Context, mediaRefs []string) []string {
	if len(mediaRefs) == 0 {
		return nil
	}
	out := make([]string, 0, len(mediaRefs))
	for _, ref := range mediaRefs {
		ref = strings.TrimSpace(ref)
		if strings.HasPrefix(ref, "feishu:image:") {
			key := strings.TrimPrefix(ref, "feishu:image:")
			if path, err := c.downloadFeishuMediaByKey(ctx, "image", key); err == nil {
				out = append(out, path)
			} else {
				logger.WarnCF("feishu", logger.C0050, map[string]interface{}{logger.FieldError: err.Error(), "image_key": key})
				out = append(out, ref)
			}
			continue
		}
		if strings.HasPrefix(ref, "feishu:file:") {
			key := strings.TrimPrefix(ref, "feishu:file:")
			if path, err := c.downloadFeishuMediaByKey(ctx, "file", key); err == nil {
				out = append(out, path)
			} else {
				logger.WarnCF("feishu", logger.C0051, map[string]interface{}{logger.FieldError: err.Error(), "file_key": key})
				out = append(out, ref)
			}
			continue
		}
		out = append(out, ref)
	}
	return out
}

func (c *FeishuChannel) downloadFeishuMediaByKey(ctx context.Context, kind, key string) (string, error) {
	dir := filepath.Join(os.TempDir(), "clawgo_feishu_media")
	_ = os.MkdirAll(dir, 0755)

	switch kind {
	case "image":
		req := larkim.NewGetImageReqBuilder().ImageKey(key).Build()
		resp, err := c.client.Im.V1.Image.Get(ctx, req)
		if err != nil {
			return "", fmt.Errorf("download feishu image failed: %w", err)
		}
		if !resp.Success() {
			return "", fmt.Errorf("download feishu image failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		path := filepath.Join(dir, resp.FileName)
		if err := resp.WriteFile(path); err != nil {
			return "", fmt.Errorf("write feishu image file failed: %w", err)
		}
		return path, nil
	case "file":
		req := larkim.NewGetFileReqBuilder().FileKey(key).Build()
		resp, err := c.client.Im.V1.File.Get(ctx, req)
		if err != nil {
			return "", fmt.Errorf("download feishu file failed: %w", err)
		}
		if !resp.Success() {
			return "", fmt.Errorf("download feishu file failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		path := filepath.Join(dir, resp.FileName)
		if err := resp.WriteFile(path); err != nil {
			return "", fmt.Errorf("write feishu file failed: %w", err)
		}
		return path, nil
	default:
		return "", fmt.Errorf("unsupported media kind: %s", kind)
	}
}

func (c *FeishuChannel) isAllowedChat(chatID, chatType string) bool {
	chatID = strings.TrimSpace(chatID)
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	isGroup := chatType != "" && chatType != "p2p"
	if isGroup && !c.config.EnableGroups {
		return false
	}
	if len(c.config.AllowChats) == 0 {
		return true
	}
	for _, allowed := range c.config.AllowChats {
		if strings.TrimSpace(allowed) == chatID {
			return true
		}
	}
	return false
}

func (c *FeishuChannel) shouldHandleGroupMessage(chatType, content string) bool {
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	isGroup := chatType != "" && chatType != "p2p"
	if !isGroup {
		return true
	}
	if !c.config.RequireMentionInGroups {
		return true
	}
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "/") {
		return true
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "@") || strings.Contains(lower, "<at") {
		return true
	}
	return false
}

func (c *FeishuChannel) sendFeishuMessage(ctx context.Context, chatID, msgType, content string) error {
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType(msgType).
			Content(content).
			Uuid(fmt.Sprintf("clawgo-%d", time.Now().UnixNano())).
			Build()).
		Build()
	resp, err := c.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send feishu message: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu api error: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (c *FeishuChannel) buildFeishuMediaOutbound(ctx context.Context, media string) (string, string, error) {
	name, data, err := readFeishuMedia(media)
	if err != nil {
		return "", "", err
	}
	ext := strings.ToLower(filepath.Ext(name))
	isImage := ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" || ext == ".gif" || ext == ".bmp"
	if isImage {
		imgReq := larkim.NewCreateImageReqBuilder().
			Body(larkim.NewCreateImageReqBodyBuilder().
				ImageType("message").
				Image(bytes.NewReader(data)).
				Build()).
			Build()
		imgResp, err := c.client.Im.Image.Create(ctx, imgReq)
		if err != nil {
			return "", "", fmt.Errorf("failed to upload feishu image: %w", err)
		}
		if !imgResp.Success() {
			return "", "", fmt.Errorf("feishu image upload error: code=%d msg=%s", imgResp.Code, imgResp.Msg)
		}
		b, _ := json.Marshal(imgResp.Data)
		return larkim.MsgTypeImage, string(b), nil
	}

	fileReq := larkim.NewCreateFileReqBuilder().
		Body(larkim.NewCreateFileReqBodyBuilder().
			FileType("stream").
			FileName(name).
			Duration(0).
			File(bytes.NewReader(data)).
			Build()).
		Build()
	fileResp, err := c.client.Im.File.Create(ctx, fileReq)
	if err != nil {
		return "", "", fmt.Errorf("failed to upload feishu file: %w", err)
	}
	if !fileResp.Success() {
		return "", "", fmt.Errorf("feishu file upload error: code=%d msg=%s", fileResp.Code, fileResp.Msg)
	}
	b, _ := json.Marshal(fileResp.Data)
	return larkim.MsgTypeFile, string(b), nil
}

func (c *FeishuChannel) buildFeishuFileFromBytes(ctx context.Context, name string, data []byte) (string, string, error) {
	fileReq := larkim.NewCreateFileReqBuilder().
		Body(larkim.NewCreateFileReqBodyBuilder().
			FileType("stream").
			FileName(name).
			Duration(0).
			File(bytes.NewReader(data)).
			Build()).
		Build()
	fileResp, err := c.client.Im.File.Create(ctx, fileReq)
	if err != nil {
		return "", "", fmt.Errorf("failed to upload feishu file: %w", err)
	}
	if !fileResp.Success() {
		return "", "", fmt.Errorf("feishu file upload error: code=%d msg=%s", fileResp.Code, fileResp.Msg)
	}
	b, _ := json.Marshal(fileResp.Data)
	return larkim.MsgTypeFile, string(b), nil
}

func readFeishuMedia(media string) (string, []byte, error) {
	if strings.HasPrefix(media, "http://") || strings.HasPrefix(media, "https://") {
		req, err := http.NewRequest(http.MethodGet, media, nil)
		if err != nil {
			return "", nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", nil, fmt.Errorf("download media failed: status=%d", resp.StatusCode)
		}
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", nil, err
		}
		name := filepath.Base(req.URL.Path)
		if strings.TrimSpace(name) == "" || name == "." || name == "/" {
			name = "media.bin"
		}
		return name, b, nil
	}
	b, err := os.ReadFile(media)
	if err != nil {
		return "", nil, err
	}
	return filepath.Base(media), b, nil
}

func buildFeishuOutbound(msg bus.OutboundMessage) (string, string, error) {
	content := strings.TrimSpace(msg.Content)

	// Support Feishu interactive card when content is raw card JSON.
	if looksLikeFeishuCard(content) {
		return larkim.MsgTypeInteractive, content, nil
	}

	if looksLikeMarkdown(content) || strings.Contains(content, "\n") || len([]rune(content)) > 180 {
		postPayload, err := buildFeishuPostContent(content)
		if err != nil {
			return "", "", fmt.Errorf("failed to marshal feishu post content: %w", err)
		}
		return larkim.MsgTypePost, postPayload, nil
	}

	textPayload, err := json.Marshal(map[string]string{"text": normalizeFeishuText(content)})
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal feishu text content: %w", err)
	}
	return larkim.MsgTypeText, string(textPayload), nil
}

func looksLikeFeishuCard(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || (!strings.HasPrefix(s, "{") && !strings.HasPrefix(s, "[")) {
		return false
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return false
	}
	_, hasElements := obj["elements"]
	_, hasHeader := obj["header"]
	_, hasConfig := obj["config"]
	return hasElements || hasHeader || hasConfig
}

func looksLikeMarkdown(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	markers := []string{"```", "# ", "## ", "- ", "* ", "**", "`", "[", "]("}
	for _, m := range markers {
		if strings.Contains(trimmed, m) {
			return true
		}
	}
	return false
}

type feishuElement map[string]interface{}
type feishuParagraph []feishuElement

type feishuTableData struct {
	Name string
	Rows [][]string
}

var (
	feishuLinkRe      = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)]+)\)`)
	feishuImgRe       = regexp.MustCompile(`!\[([^\]]*)\]\((img_[a-zA-Z0-9_-]+)\)`)
	feishuAtRe        = regexp.MustCompile(`@([a-zA-Z0-9_-]+)`)
	feishuHrRe        = regexp.MustCompile(`^\s*-{3,}\s*$`)
	feishuOrderedRe   = regexp.MustCompile(`^(\d+\.\s+)(.*)$`)
	feishuUnorderedRe = regexp.MustCompile(`^([-*]\s+)(.*)$`)
)

func (c *FeishuChannel) getTenantAccessToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	now := time.Now()
	if c.tenantAccessToken != "" && now.Before(c.tenantTokenExpire) {
		return c.tenantAccessToken, nil
	}

	// Token is missing or expired; reset once so only one fetch happens for this refresh window.
	c.tenantTokenOnce = sync.Once{}
	c.tenantTokenOnce.Do(func() {
		token, expireAt, err := c.requestTenantAccessToken(ctx)
		c.tenantAccessToken = token
		c.tenantTokenExpire = expireAt
		c.tenantTokenErr = err
	})
	if c.tenantTokenErr != nil {
		// Allow next call to retry when current refresh fails.
		c.tenantTokenOnce = sync.Once{}
		return "", c.tenantTokenErr
	}
	return c.tenantAccessToken, nil
}

func (c *FeishuChannel) requestTenantAccessToken(ctx context.Context) (string, time.Time, error) {
	req := larkauthv3.NewInternalTenantAccessTokenReqBuilder().
		Body(larkauthv3.NewInternalTenantAccessTokenReqBodyBuilder().
			AppId(c.config.AppID).
			AppSecret(c.config.AppSecret).
			Build()).
		Build()
	resp, err := c.client.Auth.V3.TenantAccessToken.Internal(ctx, req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("get tenant access token failed: %w", err)
	}
	if !resp.Success() {
		return "", time.Time{}, fmt.Errorf("get tenant access token failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var tokenResp struct {
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int64  `json:"expire"`
	}
	if err := json.Unmarshal(resp.RawBody, &tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("decode tenant access token failed: %w", err)
	}
	if strings.TrimSpace(tokenResp.TenantAccessToken) == "" {
		return "", time.Time{}, fmt.Errorf("empty tenant access token")
	}
	if tokenResp.Expire <= 0 {
		return "", time.Time{}, fmt.Errorf("invalid token expire: %d", tokenResp.Expire)
	}
	expireAt := time.Now().Add(time.Duration(tokenResp.Expire) * time.Second)
	return strings.TrimSpace(tokenResp.TenantAccessToken), expireAt, nil
}

func (c *FeishuChannel) setFeishuSheetPublicEditable(ctx context.Context, sheetToken string) error {
	req := larkdrivev2.NewPatchPermissionPublicReqBuilder().
		Token(sheetToken).
		Type("sheet").
		PermissionPublic(larkdrivev2.NewPermissionPublicBuilder().
			ExternalAccessEntity(larkdrivev2.PermissionPublicExternalAccessEntityOpen).
			SecurityEntity(larkdrivev2.PermissionPublicSecurityEntityAnyoneCanEdit).
			LinkShareEntity(larkdrivev2.PermissionPublicLinkShareEntityTenantEditable).
			Build()).
		Build()
	resp, err := c.client.Drive.V2.PermissionPublic.Patch(ctx, req)
	if err != nil {
		return fmt.Errorf("set sheet permission failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("set sheet permission failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (c *FeishuChannel) createFeishuSheetFromTable(ctx context.Context, name string, rows [][]string) (string, error) {
	createReq := larksheets.NewCreateSpreadsheetReqBuilder().
		Spreadsheet(larksheets.NewSpreadsheetBuilder().
			Title(name).
			Build()).
		Build()
	createResp, err := c.client.Sheets.V3.Spreadsheet.Create(ctx, createReq)
	if err != nil {
		return "", fmt.Errorf("create sheet failed: %w", err)
	}
	if !createResp.Success() {
		return "", fmt.Errorf("create sheet failed: code=%d msg=%s", createResp.Code, createResp.Msg)
	}
	if createResp.Data == nil || createResp.Data.Spreadsheet == nil || createResp.Data.Spreadsheet.SpreadsheetToken == nil {
		return "", fmt.Errorf("create sheet failed: spreadsheet token is empty")
	}
	spToken := strings.TrimSpace(*createResp.Data.Spreadsheet.SpreadsheetToken)
	sheetID := ""
	queryReq := larksheets.NewQuerySpreadsheetSheetReqBuilder().
		SpreadsheetToken(spToken).
		Build()
	queryResp, qerr := c.client.Sheets.V3.SpreadsheetSheet.Query(ctx, queryReq)
	if qerr == nil && queryResp != nil && queryResp.Success() && queryResp.Data != nil && len(queryResp.Data.Sheets) > 0 && queryResp.Data.Sheets[0] != nil && queryResp.Data.Sheets[0].SheetId != nil {
		sheetID = strings.TrimSpace(*queryResp.Data.Sheets[0].SheetId)
	}
	if sheetID == "" {
		sheetID = parseSheetIDFromCreateResp(createResp.RawBody)
	}
	if sheetID == "" {
		return "", fmt.Errorf("create sheet failed: empty sheet id")
	}

	if len(rows) > 0 {
		tok, err := c.getTenantAccessToken(ctx)
		if err != nil {
			return "", err
		}
		maxCols := 0
		for _, row := range rows {
			if len(row) > maxCols {
				maxCols = len(row)
			}
		}
		if maxCols == 0 {
			maxCols = 1
		}
		writeRange := fmt.Sprintf("%s!A1:%s%d", sheetID, feishuSheetColumnName(maxCols), len(rows))
		payload := map[string]interface{}{
			"valueRanges": []map[string]interface{}{{
				"range":  writeRange,
				"values": rows,
			}},
		}
		vb, _ := json.Marshal(payload)
		vreq, _ := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("https://open.feishu.cn/open-apis/sheets/v2/spreadsheets/%s/values_batch_update", spToken), bytes.NewReader(vb))
		vreq.Header.Set("Authorization", "Bearer "+tok)
		vreq.Header.Set("Content-Type", "application/json")
		vresp, err := http.DefaultClient.Do(vreq)
		if err != nil {
			return "", fmt.Errorf("write sheet values failed: %w", err)
		}
		defer vresp.Body.Close()
		vrb, _ := io.ReadAll(vresp.Body)
		var vobj map[string]interface{}
		if err := json.Unmarshal(vrb, &vobj); err != nil {
			return "", fmt.Errorf("write sheet values decode failed: %w", err)
		}
		if code, _ := vobj["code"].(float64); code != 0 {
			return "", fmt.Errorf("write sheet values code=%v msg=%v", vobj["code"], vobj["msg"])
		}
	}
	if err := c.setFeishuSheetPublicEditable(ctx, spToken); err != nil {
		logger.WarnCF("feishu", logger.C0052, map[string]interface{}{logger.FieldError: err.Error(), "sheet_token": spToken})
	}
	if createResp.Data.Spreadsheet.Url != nil && strings.TrimSpace(*createResp.Data.Spreadsheet.Url) != "" {
		return strings.TrimSpace(*createResp.Data.Spreadsheet.Url), nil
	}
	return "https://feishu.cn/sheets/" + spToken, nil
}

func parseSheetIDFromCreateResp(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	data, _ := obj["data"].(map[string]interface{})
	if data == nil {
		return ""
	}
	if sp, ok := data["spreadsheet"].(map[string]interface{}); ok {
		if sheetID, ok := sp["sheet_id"].(string); ok && strings.TrimSpace(sheetID) != "" {
			return strings.TrimSpace(sheetID)
		}
	}
	if sheetID, ok := data["sheet_id"].(string); ok && strings.TrimSpace(sheetID) != "" {
		return strings.TrimSpace(sheetID)
	}
	if sheetIDs, ok := data["sheet_ids"].([]interface{}); ok && len(sheetIDs) > 0 {
		if first, ok := sheetIDs[0].(string); ok && strings.TrimSpace(first) != "" {
			return strings.TrimSpace(first)
		}
	}
	return ""
}

func feishuSheetColumnName(col int) string {
	if col <= 0 {
		return "A"
	}
	var out []byte
	for col > 0 {
		col--
		out = append([]byte{byte('A' + (col % 26))}, out...)
		col /= 26
	}
	return string(out)
}

func parseMarkdownTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func isMarkdownTableSeparator(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.Contains(line, "|") {
		return false
	}
	line = strings.Trim(line, "| ")
	if line == "" {
		return false
	}
	for _, ch := range line {
		if ch != '-' && ch != ':' && ch != '|' && ch != ' ' {
			return false
		}
	}
	return true
}

func extractMarkdownTables(content string) (string, []feishuTableData) {
	lines := strings.Split(content, "\n")
	tables := make([]feishuTableData, 0)
	out := make([]string, 0, len(lines))
	tableIdx := 0
	for i := 0; i < len(lines); {
		line := lines[i]
		if i+1 < len(lines) && strings.Contains(line, "|") && isMarkdownTableSeparator(lines[i+1]) {
			head := parseMarkdownTableRow(line)
			rows := [][]string{head}
			i += 2
			for i < len(lines) {
				if !strings.Contains(lines[i], "|") || strings.TrimSpace(lines[i]) == "" {
					break
				}
				rows = append(rows, parseMarkdownTableRow(lines[i]))
				i++
			}
			if len(rows) >= 2 {
				tableIdx++
				name := fmt.Sprintf("table_%d", tableIdx)
				tables = append(tables, feishuTableData{Name: name, Rows: rows})
				// keep message clean: do not inject table placeholder text
				continue
			}
		}
		out = append(out, line)
		i++
	}
	return strings.Join(out, "\n"), tables
}

func parseFeishuMarkdownLine(line string) feishuParagraph {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	if feishuHrRe.MatchString(line) {
		return feishuParagraph{{"tag": "hr"}}
	}
	elems := make(feishuParagraph, 0, 5)

	if mm := feishuOrderedRe.FindStringSubmatch(line); len(mm) == 3 {
		elems = append(elems, feishuElement{"tag": "text", "text": mm[1], "style": []string{}})
		line = mm[2]
	} else if mm := feishuUnorderedRe.FindStringSubmatch(line); len(mm) == 3 {
		elems = append(elems, feishuElement{"tag": "text", "text": mm[1], "style": []string{}})
		line = mm[2]
	}

	line = feishuImgRe.ReplaceAllStringFunc(line, func(m string) string {
		mm := feishuImgRe.FindStringSubmatch(m)
		if len(mm) == 3 {
			elems = append(elems, feishuElement{"tag": "img", "image_key": mm[2]})
		}
		return ""
	})
	line = feishuLinkRe.ReplaceAllStringFunc(line, func(m string) string {
		mm := feishuLinkRe.FindStringSubmatch(m)
		if len(mm) == 3 {
			elems = append(elems, feishuElement{"tag": "a", "text": mm[1], "href": mm[2]})
		}
		return ""
	})
	line = feishuAtRe.ReplaceAllStringFunc(line, func(m string) string {
		mm := feishuAtRe.FindStringSubmatch(m)
		if len(mm) == 2 {
			elems = append(elems, feishuElement{"tag": "at", "user_id": mm[1]})
		}
		return ""
	})

	text := normalizeFeishuText(line)
	if text != "" {
		elems = append(feishuParagraph{{"tag": "text", "text": text, "style": []string{}}}, elems...)
	}
	if len(elems) == 0 {
		return nil
	}
	return elems
}

func buildFeishuPostContent(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		s = " "
	}
	lines := strings.Split(s, "\n")
	contentRows := make([]feishuParagraph, 0, len(lines)+2)
	inCode := false
	codeLang := "text"
	codeLines := make([]string, 0, 16)
	flushCode := func() {
		if len(codeLines) == 0 {
			return
		}
		contentRows = append(contentRows, feishuParagraph{{"tag": "code_block", "language": strings.ToUpper(codeLang), "text": strings.Join(codeLines, "\n")}})
		codeLines = codeLines[:0]
	}
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				flushCode()
				inCode = false
				codeLang = "text"
			} else {
				inCode = true
				lang := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
				if lang != "" {
					codeLang = lang
				}
			}
			continue
		}
		if inCode {
			codeLines = append(codeLines, line)
			continue
		}
		if p := parseFeishuMarkdownLine(line); len(p) > 0 {
			contentRows = append(contentRows, p)
		}
	}
	if inCode {
		flushCode()
	}
	if len(contentRows) == 0 {
		contentRows = append(contentRows, feishuParagraph{{"tag": "text", "text": normalizeFeishuText(s), "style": []string{}}})
	}
	payload := map[string]interface{}{
		"zh_cn": map[string]interface{}{
			"title":   "",
			"content": contentRows,
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func normalizeFeishuText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// Headers: "## title" -> "title"
	s = regexp.MustCompile(`(?m)^#{1,6}\s+`).ReplaceAllString(s, "")
	// Bullet styles
	s = regexp.MustCompile(`(?m)^[-*]\s+`).ReplaceAllString(s, "鈥?")
	// Ordered list to bullet for readability
	s = regexp.MustCompile(`(?m)^\d+\.\s+`).ReplaceAllString(s, "鈥?")
	// Bold/italic/strike markers
	s = regexp.MustCompile(`\*\*(.*?)\*\*`).ReplaceAllString(s, `$1`)
	s = regexp.MustCompile(`__(.*?)__`).ReplaceAllString(s, `$1`)
	s = regexp.MustCompile(`\*(.*?)\*`).ReplaceAllString(s, `$1`)
	s = regexp.MustCompile(`_(.*?)_`).ReplaceAllString(s, `$1`)
	s = regexp.MustCompile(`~~(.*?)~~`).ReplaceAllString(s, `$1`)
	// Inline code markers keep content
	s = regexp.MustCompile("`([^`]+)`").ReplaceAllString(s, "$1")
	// Markdown link: [text](url) -> text (url)
	s = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`).ReplaceAllString(s, `$1 ($2)`)
	return strings.TrimSpace(s)
}

func extractFeishuSenderID(sender *larkim.EventSender) string {
	if sender == nil || sender.SenderId == nil {
		return ""
	}

	if sender.SenderId.UserId != nil && *sender.SenderId.UserId != "" {
		return *sender.SenderId.UserId
	}
	if sender.SenderId.OpenId != nil && *sender.SenderId.OpenId != "" {
		return *sender.SenderId.OpenId
	}
	if sender.SenderId.UnionId != nil && *sender.SenderId.UnionId != "" {
		return *sender.SenderId.UnionId
	}

	return ""
}

func extractFeishuMessageContent(message *larkim.EventMessage) (string, []string) {
	if message == nil || message.Content == nil || *message.Content == "" {
		return "", nil
	}
	raw := *message.Content
	msgType := ""
	if message.MessageType != nil {
		msgType = *message.MessageType
	}

	switch msgType {
	case larkim.MsgTypeText:
		var textPayload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(raw), &textPayload); err == nil {
			return textPayload.Text, nil
		}
	case larkim.MsgTypePost:
		md, media := parseFeishuPostToMarkdown(raw)
		if md != "" || len(media) > 0 {
			return md, media
		}
	case larkim.MsgTypeImage:
		var img struct {
			ImageKey string `json:"image_key"`
		}
		if err := json.Unmarshal([]byte(raw), &img); err == nil && img.ImageKey != "" {
			return "[image]", []string{"feishu:image:" + img.ImageKey}
		}
	case larkim.MsgTypeFile:
		var f struct {
			FileKey  string `json:"file_key"`
			FileName string `json:"file_name"`
		}
		if err := json.Unmarshal([]byte(raw), &f); err == nil && f.FileKey != "" {
			name := f.FileName
			if name == "" {
				name = f.FileKey
			}
			return "[file] " + name, []string{"feishu:file:" + f.FileKey}
		}
	}

	return raw, nil
}

func parseFeishuPostToMarkdown(raw string) (string, []string) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return raw, nil
	}
	lang, _ := obj["zh_cn"].(map[string]interface{})
	if lang == nil {
		for _, v := range obj {
			if m, ok := v.(map[string]interface{}); ok {
				lang = m
				break
			}
		}
	}
	if lang == nil {
		return raw, nil
	}
	lines := make([]string, 0, 32)
	media := make([]string, 0, 8)
	if title, _ := lang["title"].(string); title != "" {
		lines = append(lines, "# "+title)
	}
	rows, _ := lang["content"].([]interface{})
	for _, row := range rows {
		elems, _ := row.([]interface{})
		if len(elems) == 0 {
			lines = append(lines, "")
			continue
		}
		line := ""
		for _, ev := range elems {
			em, _ := ev.(map[string]interface{})
			if em == nil {
				continue
			}
			tag := fmt.Sprintf("%v", em["tag"])
			switch tag {
			case "text", "TEXT":
				line += fmt.Sprintf("%v", em["text"])
			case "a", "A":
				line += fmt.Sprintf("[%v](%v)", em["text"], em["href"])
			case "at", "AT":
				uid := fmt.Sprintf("%v", em["user_id"])
				if uid == "" {
					uid = fmt.Sprintf("%v", em["open_id"])
				}
				line += "@" + uid
			case "img", "IMG":
				k := fmt.Sprintf("%v", em["image_key"])
				if k != "" {
					media = append(media, "feishu:image:"+k)
					line += " ![image](" + k + ")"
				}
			case "code_block", "CODE_BLOCK":
				lang := fmt.Sprintf("%v", em["language"])
				code := fmt.Sprintf("%v", em["text"])
				if line != "" {
					lines = append(lines, line)
					line = ""
				}
				lines = append(lines, "```"+lang+"\n"+code+"\n```")
			case "hr", "HR":
				if line != "" {
					lines = append(lines, line)
					line = ""
				}
				lines = append(lines, "---")
			}
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n"), media
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
