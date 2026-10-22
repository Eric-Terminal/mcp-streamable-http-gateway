package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/config"
	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/security"
)

func TestStreamableGatewayClosesBackendAfterImmediateDelete(t *testing.T) {
	bin := buildTestServer(t)
	for _, initialized := range []bool{false, true} {
		name := "初始化后立即删除"
		if initialized {
			name = "完成握手后立即删除"
		}
		t.Run(name, func(t *testing.T) {
			exitFile := filepath.Join(t.TempDir(), "backend-exited")
			cfg := &config.Config{
				AuthToken: "test-token", KeepaliveInterval: 30, SessionTimeout: 1,
				Services: map[string]config.ServiceConfig{
					"test": {Command: bin, Env: map[string]string{"MCP_TEST_EXIT_FILE": exitFile}},
				},
			}
			fail2ban := security.NewFail2Ban()
			defer fail2ban.Stop()
			gateway := NewServer(cfg, security.NewAuth(cfg.AuthToken, fail2ban))
			server := httptest.NewServer(http.HandlerFunc(gateway.handleStreamable))
			defer server.Close()
			client := bearerClient(cfg.AuthToken)
			sessionID := ""
			request := func(method, body string, want int) {
				t.Helper()
				req, err := http.NewRequest(method, server.URL+"/mcp/test", strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json, text/event-stream")
				if sessionID != "" {
					req.Header.Set("Mcp-Session-Id", sessionID)
				}
				res, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				data, err := io.ReadAll(res.Body)
				if err != nil {
					t.Fatal(err)
				}
				if res.StatusCode != want {
					t.Fatalf("%s 状态码 = %d，期望 %d：%s", method, res.StatusCode, want, data)
				}
				if id := res.Header.Get("Mcp-Session-Id"); id != "" {
					sessionID = id
				}
			}
			request(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"短会话测试","version":"1"}}}`, http.StatusOK)
			if sessionID == "" {
				t.Fatal("初始化没有返回会话 ID")
			}
			if initialized {
				request(http.MethodPost, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, http.StatusAccepted)
			}
			request(http.MethodDelete, "", http.StatusNoContent)
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if _, err := os.Stat(exitFile); err == nil {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("删除 HTTP 会话后，stdio 后端进程仍未退出")
		})
	}
}
