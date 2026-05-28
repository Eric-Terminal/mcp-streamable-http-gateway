package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/config"
	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/logger"
	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/security"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStreamableGatewayForwardsToolCall(t *testing.T) {
	logger.SetOutput(os.Stderr)
	logger.SetColor(false)

	bin := buildTestServer(t)
	cfg := &config.Config{
		AuthToken:         "test-token",
		ListenAddr:        ":0",
		KeepaliveInterval: 30,
		Services: map[string]config.ServiceConfig{
			"test": {
				Command: bin,
			},
		},
	}

	fail2ban := security.NewFail2Ban()
	defer fail2ban.Stop()

	server := NewServer(cfg, security.NewAuth(cfg.AuthToken, fail2ban))
	httpServer := httptest.NewServer(http.HandlerFunc(server.handleStreamable))
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "gateway-test-client",
		Version: "0.1.0",
	}, nil)

	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL + "/mcp/test",
		HTTPClient:           bearerClient(cfg.AuthToken),
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("连接网关失败: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("读取工具列表失败: %v", err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "echo" {
		t.Fatalf("工具列表不符合预期: %#v", tools.Tools)
	}

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"message": "hello"},
	})
	if err != nil {
		t.Fatalf("调用工具失败: %v", err)
	}

	got, ok := res.StructuredContent.(map[string]any)
	if !ok || got["message"] != "hello" {
		t.Fatalf("工具响应不符合预期: %#v", res.StructuredContent)
	}
}

func TestStreamableGatewayRejectsMissingAuth(t *testing.T) {
	cfg := &config.Config{
		AuthToken:         "test-token",
		ListenAddr:        ":0",
		KeepaliveInterval: 30,
		Services: map[string]config.ServiceConfig{
			"test": {Command: "unused"},
		},
	}
	fail2ban := security.NewFail2Ban()
	defer fail2ban.Stop()

	server := NewServer(cfg, security.NewAuth(cfg.AuthToken, fail2ban))
	req := httptest.NewRequest(http.MethodPost, "/mcp/test", nil)
	rec := httptest.NewRecorder()

	server.handleStreamable(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d，想要 %d", rec.Code, http.StatusNotFound)
	}
}

type authTransport struct {
	token string
	base  http.RoundTripper
}

func bearerClient(token string) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: authTransport{
			token: token,
			base:  http.DefaultTransport,
		},
	}
}

func (t authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}

func buildTestServer(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "test-stdio-server")
	cmd := exec.Command("go", "build", "-o", bin, "./internal/testserver")
	cmd.Dir = moduleRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("构建测试 MCP 服务失败: %v\n%s", err, out)
	}
	return bin
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatal("找不到 go.mod")
		}
		wd = parent
	}
}
