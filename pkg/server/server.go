package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"gitea.kkkk.dev/DBT/clawgo/pkg/config"
	"gitea.kkkk.dev/DBT/clawgo/pkg/logger"
)

type Server struct {
	server *http.Server
	config *config.Config
}

func NewServer(cfg *config.Config) *Server {
	return &Server{
		config: cfg,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/", s.handleRoot)

	addr := fmt.Sprintf("%s:%d", s.config.Gateway.Host, s.config.Gateway.Port)
	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	logger.InfoCF("server", "Starting HTTP server", map[string]interface{}{
		"addr": addr,
	})
	
	// Check/log indicating it's ready for reverse proxying (per requirement)
	logger.InfoC("server", "Server ready for reverse proxying")

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.ErrorCF("server", "HTTP server failed", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		logger.InfoC("server", "Stopping HTTP server")
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "ClawGo Gateway Running\nTime: %s", time.Now().Format(time.RFC3339))
}
