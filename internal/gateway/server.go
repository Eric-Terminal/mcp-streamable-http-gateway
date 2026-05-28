package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/config"
	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/logger"
	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/security"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const Version = "0.1.0"

type Server struct {
	config     *config.Config
	auth       *security.Auth
	httpServer *http.Server
	handlers   map[string]*mcp.StreamableHTTPHandler
}

func NewServer(cfg *config.Config, auth *security.Auth) *Server {
	handlers := make(map[string]*mcp.StreamableHTTPHandler, len(cfg.Services))
	for name, svc := range cfg.Services {
		bridge := newServiceBridge(name, cfg, svc)
		opts := &mcp.StreamableHTTPOptions{
			JSONResponse:               true,
			SessionTimeout:             time.Duration(cfg.SessionTimeout) * time.Second,
			DisableLocalhostProtection: true,
		}
		handlers[name] = mcp.NewStreamableHTTPHandler(bridge.NewServer, opts)
	}

	return &Server{
		config:   cfg,
		auth:     auth,
		handlers: handlers,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/", s.handleStreamable)
	mux.HandleFunc("/sse/", s.handleLegacyAlias)

	s.httpServer = &http.Server{
		Addr:         s.config.ListenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	logger.Gateway("MCP Streamable HTTP Gateway 启动于 %s", s.config.ListenAddr)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleStreamable(w http.ResponseWriter, r *http.Request) {
	serviceName := serviceNameFromPath(r.URL.Path, "/mcp/")
	if serviceName == "" {
		security.Fake404Response(w)
		return
	}
	s.serveService(w, r, serviceName)
}

func (s *Server) handleLegacyAlias(w http.ResponseWriter, r *http.Request) {
	serviceName := serviceNameFromPath(r.URL.Path, "/sse/")
	if serviceName == "" {
		security.Fake404Response(w)
		return
	}
	s.serveService(w, r, serviceName)
}

func (s *Server) serveService(w http.ResponseWriter, r *http.Request, serviceName string) {
	if !s.auth.ValidateRequest(r) {
		security.Fake404Response(w)
		return
	}

	handler := s.handlers[serviceName]
	if handler == nil {
		security.Fake404Response(w)
		return
	}

	logger.Gateway("[HTTP] ip=%s service=%s method=%s path=%s", security.ExtractIP(r), serviceName, r.Method, r.URL.Path)
	handler.ServeHTTP(w, r)
}

func serviceNameFromPath(path, prefix string) string {
	name := strings.TrimPrefix(path, prefix)
	if name == path {
		return ""
	}
	name = strings.Trim(name, "/")
	if name == "" || strings.Contains(name, "/") {
		return ""
	}
	return name
}
