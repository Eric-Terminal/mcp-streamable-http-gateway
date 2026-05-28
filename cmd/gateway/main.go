package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/config"
	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/gateway"
	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/logger"
	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/security"
)

var defaultConfigPaths = []string{
	"config.json",
	"./config.json",
	"./config/config.json",
	"/etc/mcp-streamable-http-gateway/config.json",
	"/etc/mcp-sse-gateway/config.json",
}

func main() {
	configPath := flag.String("config", "", "配置文件路径（默认自动查找）")
	logLevel := flag.String("log-level", "info", "日志级别: debug, info, warn, error")
	logFile := flag.String("log-file", "", "日志文件路径（默认: 与可执行文件同目录的 gateway.log）")
	noLog := flag.Bool("no-log", false, "禁用日志文件输出")
	noColor := flag.Bool("no-color", false, "禁用日志颜色")
	flag.Parse()

	logger.SetLevel(logger.ParseLevel(*logLevel))
	if *noColor {
		logger.SetColor(false)
	}

	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = findConfigFile()
		if cfgPath == "" {
			logger.Error("找不到配置文件，请指定 -config 参数")
			logger.Error("已搜索: %v", defaultConfigPaths)
			os.Exit(1)
		}
	}

	if !*noLog {
		logPath := *logFile
		if logPath == "" {
			exePath, err := os.Executable()
			if err != nil {
				exePath = "."
			}
			logPath = filepath.Join(filepath.Dir(exePath), "gateway.log")
		}
		closeLog, err := logger.SetLogFile(logPath)
		if err != nil {
			logger.Error("设置日志文件失败: %v", err)
			os.Exit(1)
		}
		defer closeLog()
		logger.System("日志文件: %s", logPath)
	}

	logger.System("配置文件: %s", cfgPath)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		logger.Error("加载配置失败: %v", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		logger.Error("配置错误: %v", err)
		os.Exit(1)
	}

	logger.Config("配置加载成功: %d 个服务", len(cfg.Services))
	for name, svc := range cfg.Services {
		logger.Config("  - %s: %s %v", name, svc.Command, svc.Args)
	}

	fail2ban := security.NewFail2Ban()
	defer fail2ban.Stop()
	logger.Security("Fail2Ban 已启用: 1分钟内>10次错误 → 封禁1小时")

	auth := security.NewAuth(cfg.AuthToken, fail2ban)
	server := gateway.NewServer(cfg, auth)

	errChan := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		logger.System("收到关闭信号 %s，正在优雅关闭...", sig)
	case err := <-errChan:
		logger.Error("服务器启动错误: %v", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("关闭错误: %v", err)
	}
	logger.System("服务器已关闭")
}

func findConfigFile() string {
	for _, path := range defaultConfigPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}
