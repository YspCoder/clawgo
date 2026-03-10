package channels

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
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
	return bridgeEndpointURL(raw, "status")
}

func BridgeLogoutURL(raw string) (string, error) {
	return bridgeEndpointURL(raw, "logout")
}

func bridgeEndpointURL(raw, endpoint string) (string, error) {
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
	u.Path = bridgeSiblingPath(u.Path, endpoint)
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func bridgeSiblingPath(pathValue, endpoint string) string {
	pathValue = strings.TrimSpace(pathValue)
	if endpoint == "" {
		endpoint = "status"
	}
	if pathValue == "" || pathValue == "/" {
		return "/" + endpoint
	}
	trimmed := strings.TrimSuffix(pathValue, "/")
	if strings.HasSuffix(trimmed, "/ws") {
		return strings.TrimSuffix(trimmed, "/ws") + "/" + endpoint
	}
	return trimmed + "/" + endpoint
}

func normalizeBridgeBasePath(basePath string) string {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" || basePath == "/" {
		return "/"
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	return strings.TrimSuffix(basePath, "/")
}

func joinBridgeRoute(basePath, endpoint string) string {
	basePath = normalizeBridgeBasePath(basePath)
	if basePath == "/" {
		return "/" + strings.TrimPrefix(endpoint, "/")
	}
	return basePath + "/" + strings.TrimPrefix(endpoint, "/")
}

func isLocalRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	return isLocalRemoteAddr(strings.TrimSpace(r.RemoteAddr), addrs)
}

func isLocalRemoteAddr(remoteAddr string, localAddrs []net.Addr) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		host = strings.TrimSpace(remoteAddr)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, addr := range localAddrs {
		if addr == nil {
			continue
		}
		switch v := addr.(type) {
		case *net.IPNet:
			if v.IP != nil && v.IP.Equal(ip) {
				return true
			}
		case *net.IPAddr:
			if v.IP != nil && v.IP.Equal(ip) {
				return true
			}
		}
	}
	return false
}
