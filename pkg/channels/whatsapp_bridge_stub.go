//go:build omit_whatsapp

package channels

import (
	"context"
	"net/http"
	"strings"
)

type WhatsAppBridgeService struct {
	addr     string
	stateDir string
	printQR  bool
	status   WhatsAppBridgeStatus
}

func NewWhatsAppBridgeService(addr, stateDir string, printQR bool) *WhatsAppBridgeService {
	return &WhatsAppBridgeService{
		addr:     strings.TrimSpace(addr),
		stateDir: strings.TrimSpace(stateDir),
		printQR:  printQR,
		status: WhatsAppBridgeStatus{
			State:      "disabled",
			BridgeAddr: strings.TrimSpace(addr),
		},
	}
}

func (s *WhatsAppBridgeService) Start(ctx context.Context) error {
	return errChannelDisabled("whatsapp")
}

func (s *WhatsAppBridgeService) StartEmbedded(ctx context.Context) error {
	return errChannelDisabled("whatsapp")
}

func (s *WhatsAppBridgeService) Stop() {}

func (s *WhatsAppBridgeService) RegisterRoutes(mux *http.ServeMux, basePath string) {}

func (s *WhatsAppBridgeService) StatusSnapshot() WhatsAppBridgeStatus {
	return s.status
}

func (s *WhatsAppBridgeService) ServeWS(w http.ResponseWriter, r *http.Request) {
	http.Error(w, errChannelDisabled("whatsapp").Error(), http.StatusNotImplemented)
}

func (s *WhatsAppBridgeService) ServeStatus(w http.ResponseWriter, r *http.Request) {
	http.Error(w, errChannelDisabled("whatsapp").Error(), http.StatusNotImplemented)
}

func (s *WhatsAppBridgeService) ServeLogout(w http.ResponseWriter, r *http.Request) {
	http.Error(w, errChannelDisabled("whatsapp").Error(), http.StatusNotImplemented)
}
