package security

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"

	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/logger"
)

type Auth struct {
	token    []byte
	fail2ban *Fail2Ban
}

func NewAuth(token string, fail2ban *Fail2Ban) *Auth {
	return &Auth{
		token:    []byte(token),
		fail2ban: fail2ban,
	}
}

func (a *Auth) ValidateRequest(r *http.Request) bool {
	ip := ExtractIP(r)

	if a.fail2ban.IsBanned(ip) {
		logger.SecurityWarn("[IP封禁] 已封禁IP尝试访问: ip=%s path=%s", ip, r.URL.Path)
		return false
	}

	authHeader := r.Header.Get("Authorization")
	if len(authHeader) < 7 || authHeader[:7] != "Bearer " {
		logger.SecurityWarn("[鉴权失败] 缺少或无效的Authorization头: ip=%s path=%s", ip, r.URL.Path)
		a.fail2ban.RecordFailure(ip)
		return false
	}

	if subtle.ConstantTimeCompare([]byte(authHeader[7:]), a.token) != 1 {
		logger.SecurityWarn("[鉴权失败] Token不匹配: ip=%s path=%s", ip, r.URL.Path)
		a.fail2ban.RecordFailure(ip)
		return false
	}

	logger.Security("[鉴权成功] ip=%s path=%s method=%s", ip, r.URL.Path, r.Method)
	return true
}

func ExtractIP(r *http.Request) string {
	for _, header := range []string{"CF-Connecting-IP", "Fastly-Client-IP", "True-Client-IP", "X-Real-IP"} {
		if ip := strings.TrimSpace(r.Header.Get(header)); ip != "" {
			return ip
		}
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ip, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(ip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func Fake404Response(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("<!DOCTYPE html>\n<html><head><title>404 Not Found</title></head><body><h1>Not Found</h1><p>The requested URL was not found on this server.</p></body></html>"))
}
