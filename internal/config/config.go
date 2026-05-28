package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type ServiceConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

type Config struct {
	AuthToken         string                   `json:"auth_token"`
	ListenAddr        string                   `json:"listen_addr"`
	Services          map[string]ServiceConfig `json:"services"`
	KeepaliveInterval int                      `json:"keepalive_interval"`
	SessionTimeout    int                      `json:"session_timeout"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	if cfg.KeepaliveInterval <= 0 {
		cfg.KeepaliveInterval = 30
	}
	if cfg.SessionTimeout < 0 {
		cfg.SessionTimeout = 0
	}
	if cfg.Services == nil {
		cfg.Services = map[string]ServiceConfig{}
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.AuthToken) == "" {
		return fmt.Errorf("auth_token 不能为空")
	}
	if len(c.Services) == 0 {
		return fmt.Errorf("services 不能为空")
	}
	for name, svc := range c.Services {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("服务名不能为空")
		}
		if strings.TrimSpace(svc.Command) == "" {
			return fmt.Errorf("服务 %q 的 command 不能为空", name)
		}
	}
	return nil
}

func (c *Config) GetService(name string) (*ServiceConfig, bool) {
	svc, ok := c.Services[name]
	if !ok {
		return nil, false
	}
	return &svc, true
}

func (c *Config) GetServiceEnv(serviceName string) []string {
	envMap := make(map[string]string)
	for _, e := range os.Environ() {
		k, v, ok := strings.Cut(e, "=")
		if ok {
			envMap[k] = v
		}
	}

	if svc, ok := c.Services[serviceName]; ok {
		for k, v := range svc.Env {
			envMap[k] = v
		}
	}

	result := make([]string, 0, len(envMap))
	for k, v := range envMap {
		result = append(result, k+"="+v)
	}
	return result
}
