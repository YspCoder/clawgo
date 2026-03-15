package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/YspCoder/clawgo/pkg/nodes"
	"github.com/gorilla/websocket"
)

func publishLiveSnapshot(subs map[chan []byte]struct{}, payload []byte) {
	for ch := range subs {
		select {
		case ch <- payload:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- payload:
			default:
			}
		}
	}
}

func (s *Server) subscribeRuntimeLive(ctx context.Context) chan []byte {
	ch := make(chan []byte, 1)
	s.liveRuntimeMu.Lock()
	s.liveRuntimeSubs[ch] = struct{}{}
	start := !s.liveRuntimeOn
	if start {
		s.liveRuntimeOn = true
	}
	s.liveRuntimeMu.Unlock()
	if start {
		go s.runtimeLiveLoop()
	}
	go func() {
		<-ctx.Done()
		s.unsubscribeRuntimeLive(ch)
	}()
	return ch
}

func (s *Server) unsubscribeRuntimeLive(ch chan []byte) {
	s.liveRuntimeMu.Lock()
	delete(s.liveRuntimeSubs, ch)
	s.liveRuntimeMu.Unlock()
}

func (s *Server) runtimeLiveLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if !s.publishRuntimeSnapshot(context.Background()) {
			s.liveRuntimeMu.Lock()
			if len(s.liveRuntimeSubs) == 0 {
				s.liveRuntimeOn = false
				s.liveRuntimeMu.Unlock()
				return
			}
			s.liveRuntimeMu.Unlock()
		}
		<-ticker.C
	}
}

func (s *Server) publishRuntimeSnapshot(ctx context.Context) bool {
	if s == nil {
		return false
	}
	payload := map[string]interface{}{
		"ok":       true,
		"type":     "runtime_snapshot",
		"snapshot": s.buildWebUIRuntimeSnapshot(ctx),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	s.liveRuntimeMu.Lock()
	defer s.liveRuntimeMu.Unlock()
	if len(s.liveRuntimeSubs) == 0 {
		return false
	}
	publishLiveSnapshot(s.liveRuntimeSubs, data)
	return true
}

func parseLogLine(line string) (map[string]interface{}, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, false
	}
	if json.Valid([]byte(line)) {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err == nil {
			return m, true
		}
	}
	return map[string]interface{}{
		"time":  time.Now().UTC().Format(time.RFC3339),
		"level": "INFO",
		"msg":   line,
	}, true
}

func (s *Server) handleWebUILogsLive(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimSpace(s.logFilePath)
	if path == "" {
		http.Error(w, "log path not configured", http.StatusInternalServerError)
		return
	}
	conn, err := nodesWebsocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	f, err := os.Open(path)
	if err != nil {
		_ = conn.WriteJSON(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	if fi != nil {
		_, _ = f.Seek(fi.Size(), io.SeekStart)
	}
	reader := bufio.NewReader(f)
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			line, err := reader.ReadString('\n')
			if parsed, ok := parseLogLine(line); ok {
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if writeErr := conn.WriteJSON(map[string]interface{}{"ok": true, "type": "log_entry", "entry": parsed}); writeErr != nil {
					return
				}
			}
			if err != nil {
				time.Sleep(500 * time.Millisecond)
			}
		}
	}
}

type nodeSocketConn struct {
	connID string
	conn   *websocket.Conn
	mu     sync.Mutex
}

func (c *nodeSocketConn) writeJSON(payload interface{}) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("node websocket unavailable")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteJSON(payload)
}

func (c *nodeSocketConn) Send(msg nodes.WireMessage) error {
	return c.writeJSON(msg)
}
