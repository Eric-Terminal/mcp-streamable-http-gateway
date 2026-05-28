package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/config"
	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type serviceBridge struct {
	name string
	cfg  *config.Config
	svc  config.ServiceConfig
}

func newServiceBridge(name string, cfg *config.Config, svc config.ServiceConfig) *serviceBridge {
	return &serviceBridge{
		name: name,
		cfg:  cfg,
		svc:  svc,
	}
}

func (b *serviceBridge) NewServer(r *http.Request) *mcp.Server {
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "mcp-streamable-http-gateway-stdio-client",
		Version: Version,
	}, clientOptions(b.name))

	remote, err := b.connectRemote(client)
	if err != nil {
		logger.GatewayError("[后端连接失败] service=%s ip=%s error=%v", b.name, clientIP(r), err)
		return nil
	}

	opts := &mcp.ServerOptions{
		Instructions:                remote.initializeResult.Instructions,
		Capabilities:                proxyCapabilities(remote.initializeResult.Capabilities),
		InitializedHandler:          remote.initialized,
		RootsListChangedHandler:     remote.rootsListChanged,
		ProgressNotificationHandler: remote.progress,
		KeepAlive:                   time.Duration(b.cfg.KeepaliveInterval) * time.Second,
	}
	if caps := remote.initializeResult.Capabilities; caps != nil {
		if caps.Completions != nil {
			opts.CompletionHandler = remote.complete
		}
		if caps.Resources != nil && caps.Resources.Subscribe {
			opts.SubscribeHandler = remote.subscribe
			opts.UnsubscribeHandler = remote.unsubscribe
		}
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    b.name,
		Title:   fmt.Sprintf("%s via Streamable HTTP Gateway", b.name),
		Version: Version,
	}, opts)

	remote.server = server
	remote.registerFeatures(server)
	go remote.closeWhenIdle()

	return server
}

func (b *serviceBridge) connectRemote(client *mcp.Client) (*remoteSession, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, b.svc.Command, b.svc.Args...)
	cmd.Env = b.cfg.GetServiceEnv(b.name)

	transport := &mcp.CommandTransport{
		Command:           cmd,
		TerminateDuration: 5 * time.Second,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		cancel()
		return nil, err
	}

	result := session.InitializeResult()
	if result == nil {
		session.Close()
		cancel()
		return nil, errors.New("后端 MCP 初始化失败")
	}

	remote := &remoteSession{
		serviceName:       b.name,
		client:            client,
		session:           session,
		cancel:            cancel,
		initializeResult:  result,
		tools:             map[string]*mcp.Tool{},
		prompts:           map[string]*mcp.Prompt{},
		resources:         map[string]*mcp.Resource{},
		resourceTemplates: map[string]*mcp.ResourceTemplate{},
	}

	if err := remote.refreshAll(ctx); err != nil {
		session.Close()
		cancel()
		return nil, err
	}

	logger.Gateway("[会话创建] service=%s tools=%d prompts=%d resources=%d templates=%d", b.name, len(remote.tools), len(remote.prompts), len(remote.resources), len(remote.resourceTemplates))
	return remote, nil
}

func clientOptions(serviceName string) *mcp.ClientOptions {
	opts := &mcp.ClientOptions{}
	opts.ToolListChangedHandler = func(ctx context.Context, req *mcp.ToolListChangedRequest) {
		logger.Gateway("[后端工具变更] service=%s session=%s", serviceName, req.Session.ID())
	}
	opts.PromptListChangedHandler = func(ctx context.Context, req *mcp.PromptListChangedRequest) {
		logger.Gateway("[后端提示词变更] service=%s session=%s", serviceName, req.Session.ID())
	}
	opts.ResourceListChangedHandler = func(ctx context.Context, req *mcp.ResourceListChangedRequest) {
		logger.Gateway("[后端资源变更] service=%s session=%s", serviceName, req.Session.ID())
	}
	opts.ResourceUpdatedHandler = func(ctx context.Context, req *mcp.ResourceUpdatedNotificationRequest) {
		logger.Gateway("[后端资源更新] service=%s uri=%s", serviceName, req.Params.URI)
	}
	opts.LoggingMessageHandler = func(ctx context.Context, req *mcp.LoggingMessageRequest) {
		logger.GatewayDebug("[后端日志] service=%s level=%s logger=%s", serviceName, req.Params.Level, req.Params.Logger)
	}
	return opts
}

type remoteSession struct {
	serviceName string
	client      *mcp.Client
	session     *mcp.ClientSession
	cancel      context.CancelFunc
	server      *mcp.Server

	initializeResult *mcp.InitializeResult

	mu                sync.RWMutex
	tools             map[string]*mcp.Tool
	prompts           map[string]*mcp.Prompt
	resources         map[string]*mcp.Resource
	resourceTemplates map[string]*mcp.ResourceTemplate
}

func (r *remoteSession) refreshAll(ctx context.Context) error {
	if err := r.refreshTools(ctx); err != nil {
		return err
	}
	if err := r.refreshPrompts(ctx); err != nil {
		return err
	}
	if err := r.refreshResources(ctx); err != nil {
		return err
	}
	if err := r.refreshResourceTemplates(ctx); err != nil {
		return err
	}
	return nil
}

func (r *remoteSession) refreshTools(ctx context.Context) error {
	caps := r.initializeResult.Capabilities
	if caps == nil || caps.Tools == nil {
		return nil
	}

	tools := map[string]*mcp.Tool{}
	for tool, err := range r.session.Tools(ctx, nil) {
		if err != nil {
			return fmt.Errorf("读取工具列表失败: %w", err)
		}
		tools[tool.Name] = tool
	}

	r.mu.Lock()
	r.tools = tools
	r.mu.Unlock()
	return nil
}

func (r *remoteSession) refreshPrompts(ctx context.Context) error {
	caps := r.initializeResult.Capabilities
	if caps == nil || caps.Prompts == nil {
		return nil
	}

	prompts := map[string]*mcp.Prompt{}
	for prompt, err := range r.session.Prompts(ctx, nil) {
		if err != nil {
			return fmt.Errorf("读取提示词列表失败: %w", err)
		}
		prompts[prompt.Name] = prompt
	}

	r.mu.Lock()
	r.prompts = prompts
	r.mu.Unlock()
	return nil
}

func (r *remoteSession) refreshResources(ctx context.Context) error {
	caps := r.initializeResult.Capabilities
	if caps == nil || caps.Resources == nil {
		return nil
	}

	resources := map[string]*mcp.Resource{}
	for resource, err := range r.session.Resources(ctx, nil) {
		if err != nil {
			return fmt.Errorf("读取资源列表失败: %w", err)
		}
		resources[resource.URI] = resource
	}

	r.mu.Lock()
	r.resources = resources
	r.mu.Unlock()
	return nil
}

func (r *remoteSession) refreshResourceTemplates(ctx context.Context) error {
	caps := r.initializeResult.Capabilities
	if caps == nil || caps.Resources == nil {
		return nil
	}

	templates := map[string]*mcp.ResourceTemplate{}
	for template, err := range r.session.ResourceTemplates(ctx, nil) {
		if err != nil {
			return fmt.Errorf("读取资源模板列表失败: %w", err)
		}
		templates[template.URITemplate] = template
	}

	r.mu.Lock()
	r.resourceTemplates = templates
	r.mu.Unlock()
	return nil
}

func (r *remoteSession) registerFeatures(server *mcp.Server) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, tool := range r.tools {
		toolCopy := *tool
		server.AddTool(&toolCopy, r.callTool)
	}
	for _, prompt := range r.prompts {
		promptCopy := *prompt
		server.AddPrompt(&promptCopy, r.getPrompt)
	}
	for _, resource := range r.resources {
		resourceCopy := *resource
		server.AddResource(&resourceCopy, r.readResource)
	}
	for _, template := range r.resourceTemplates {
		templateCopy := *template
		server.AddResourceTemplate(&templateCopy, r.readResource)
	}
}

func (r *remoteSession) initialized(ctx context.Context, req *mcp.InitializedRequest) {
	logger.Gateway("[远端已初始化] service=%s stream_session=%s", r.serviceName, req.Session.ID())
}

func (r *remoteSession) callTool(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	params := &mcp.CallToolParams{
		Name: req.Params.Name,
	}
	if len(req.Params.Arguments) > 0 {
		var args any
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		params.Arguments = args
	}

	logger.Gateway("[工具调用] service=%s tool=%s", r.serviceName, req.Params.Name)
	res, err := r.session.CallTool(ctx, params)
	if err != nil {
		logger.GatewayError("[工具调用失败] service=%s tool=%s error=%v", r.serviceName, req.Params.Name, err)
		return nil, err
	}
	return res, nil
}

func (r *remoteSession) getPrompt(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return r.session.GetPrompt(ctx, req.Params)
}

func (r *remoteSession) readResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return r.session.ReadResource(ctx, req.Params)
}

func (r *remoteSession) complete(ctx context.Context, req *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	return r.session.Complete(ctx, req.Params)
}

func (r *remoteSession) subscribe(ctx context.Context, req *mcp.SubscribeRequest) error {
	return r.session.Subscribe(ctx, req.Params)
}

func (r *remoteSession) unsubscribe(ctx context.Context, req *mcp.UnsubscribeRequest) error {
	return r.session.Unsubscribe(ctx, req.Params)
}

func (r *remoteSession) rootsListChanged(ctx context.Context, req *mcp.RootsListChangedRequest) {
	logger.GatewayDebug("[客户端根目录变更] service=%s stream_session=%s", r.serviceName, req.Session.ID())
}

func (r *remoteSession) progress(ctx context.Context, req *mcp.ProgressNotificationServerRequest) {
	if err := r.session.NotifyProgress(ctx, req.Params); err != nil {
		logger.GatewayError("[进度转发失败] service=%s error=%v", r.serviceName, err)
	}
}

func (r *remoteSession) closeWhenIdle() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	seenOuterSession := false
	for range ticker.C {
		count := 0
		for range r.server.Sessions() {
			count++
		}
		if count > 0 {
			seenOuterSession = true
			continue
		}
		if seenOuterSession {
			r.close()
			return
		}
	}
}

func (r *remoteSession) close() {
	if err := r.session.Close(); err != nil {
		logger.GatewayError("[后端会话关闭失败] service=%s error=%v", r.serviceName, err)
	}
	r.cancel()
	logger.Gateway("[会话关闭] service=%s", r.serviceName)
}

func proxyCapabilities(caps *mcp.ServerCapabilities) *mcp.ServerCapabilities {
	if caps == nil {
		return &mcp.ServerCapabilities{}
	}

	proxy := &mcp.ServerCapabilities{
		Experimental: maps.Clone(caps.Experimental),
		Extensions:   maps.Clone(caps.Extensions),
	}
	if caps.Tools != nil {
		proxy.Tools = &mcp.ToolCapabilities{}
	}
	if caps.Prompts != nil {
		proxy.Prompts = &mcp.PromptCapabilities{}
	}
	if caps.Resources != nil {
		proxy.Resources = &mcp.ResourceCapabilities{
			Subscribe: caps.Resources.Subscribe,
		}
	}
	if caps.Completions != nil {
		proxy.Completions = &mcp.CompletionCapabilities{}
	}
	if caps.Logging != nil {
		proxy.Logging = &mcp.LoggingCapabilities{}
	}
	return proxy
}

func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
