# MCP Streamable HTTP Gateway

一个用 Go 官方 MCP SDK 实现的 Streamable HTTP 网关。它把远程 Streamable HTTP MCP 请求转发到本地 stdio MCP 服务，让 iOS、浏览器或其他不方便启动 stdio 进程的客户端继续使用只提供 stdio 的 MCP Server。

```
Client  <-- Streamable HTTP -->  Gateway  <-- stdio -->  MCP Server
```

## 特性

- 使用 `github.com/modelcontextprotocol/go-sdk` 提供 MCP 协议与 Streamable HTTP 传输实现。
- 兼容旧 `mcp-sse-gateway` 的 `config.json` 结构，服务名、命令、参数和环境变量可以直接迁移。
- 每个远程 MCP 会话对应一个后端 stdio MCP 子进程，会话关闭后自动清理。
- Bearer Token 鉴权、伪装 404、内存版 Fail2Ban 反爆破。
- 默认端点是 `/mcp/{service}`；迁移期也接受 `/sse/{service}` 作为 Streamable HTTP 处理入口。

## 协议来源

- Streamable HTTP 传输规范：https://modelcontextprotocol.io/specification/2025-06-18/basic/transports
- Go MCP SDK：https://github.com/modelcontextprotocol/go-sdk

## 快速开始

```bash
go build -o mcp-streamable-http-gateway ./cmd/gateway
cp config.example.json config.json
./mcp-streamable-http-gateway
```

客户端配置里把传输模式改成 Streamable HTTP，URL 指向：

```text
http://localhost:8080/mcp/github
```

所有请求需要带：

```text
Authorization: Bearer <auth_token>
```

## 配置

```json
{
  "auth_token": "your-secure-token",
  "listen_addr": ":8080",
  "keepalive_interval": 30,
  "session_timeout": 3600,
  "services": {
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_xxxx"
      }
    }
  }
}
```

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `auth_token` | 是 | - | Bearer Token，建议 64 字符以上 |
| `listen_addr` | 否 | `:8080` | HTTP 监听地址 |
| `keepalive_interval` | 否 | `30` | MCP 会话 keepalive 间隔，秒 |
| `session_timeout` | 否 | `0` | Streamable HTTP 空闲会话超时，秒；`0` 表示不主动超时 |
| `services` | 是 | - | 服务名到 stdio MCP 启动命令的映射 |
| `services.*.command` | 是 | - | 后端 MCP Server 命令 |
| `services.*.args` | 否 | `[]` | 命令参数 |
| `services.*.env` | 否 | `{}` | 额外环境变量，会覆盖系统环境 |

## 命令行参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-config` | 自动查找 | 配置文件路径 |
| `-log-level` | `info` | 日志级别: `debug`, `info`, `warn`, `error` |
| `-log-file` | 可执行文件同目录 `gateway.log` | 日志文件路径 |
| `-no-log` | `false` | 禁用日志文件输出 |
| `-no-color` | `false` | 禁用控制台颜色 |

配置文件自动搜索顺序：

1. `config.json`
2. `./config.json`
3. `./config/config.json`
4. `/etc/mcp-streamable-http-gateway/config.json`
5. `/etc/mcp-sse-gateway/config.json`

## 安全行为

鉴权失败、未知服务、错误路径都会返回相同的伪装 404。Token 校验使用常量时间比较；同一 IP 在 1 分钟内鉴权失败超过 10 次会封禁 1 小时。

真实生产配置可能包含 GitHub Token 等敏感信息，`config.json` 已在 `.gitignore` 中，不要提交。
