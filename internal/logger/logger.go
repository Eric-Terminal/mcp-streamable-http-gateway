package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var levelNames = map[Level]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
}

var levelColors = map[Level]string{
	LevelDebug: "\033[36m",
	LevelInfo:  "\033[32m",
	LevelWarn:  "\033[33m",
	LevelError: "\033[31m",
}

const colorReset = "\033[0m"

type Logger struct {
	mu       sync.Mutex
	out      io.Writer
	file     *os.File
	fileOut  io.Writer
	level    Level
	useColor bool
}

var defaultLogger = &Logger{
	out:      os.Stdout,
	level:    LevelInfo,
	useColor: true,
}

func SetOutput(w io.Writer) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.out = w
}

func SetLogFile(path string) (func(), error) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("打开日志文件失败: %w", err)
	}

	defaultLogger.file = f
	defaultLogger.fileOut = f

	return func() {
		defaultLogger.mu.Lock()
		defer defaultLogger.mu.Unlock()
		if defaultLogger.file != nil {
			defaultLogger.file.Close()
			defaultLogger.file = nil
			defaultLogger.fileOut = nil
		}
	}, nil
}

func SetLevel(level Level) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.level = level
}

func SetColor(use bool) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.useColor = use
}

func ParseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func (l *Logger) log(level Level, category, format string, args ...any) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().Format("2006-01-02 15:04:05.000")
	msg := fmt.Sprintf(format, args...)

	if l.useColor {
		color := levelColors[level]
		fmt.Fprintf(l.out, "%s %s[%-5s]%s [%s] %s\n", now, color, levelNames[level], colorReset, category, msg)
	} else {
		fmt.Fprintf(l.out, "%s [%-5s] [%s] %s\n", now, levelNames[level], category, msg)
	}

	if l.fileOut != nil {
		fmt.Fprintf(l.fileOut, "%s [%-5s] [%s] %s\n", now, levelNames[level], category, msg)
	}
}

func Security(format string, args ...any) { defaultLogger.log(LevelInfo, "SECURITY", format, args...) }
func SecurityWarn(format string, args ...any) {
	defaultLogger.log(LevelWarn, "SECURITY", format, args...)
}
func Gateway(format string, args ...any) { defaultLogger.log(LevelInfo, "GATEWAY", format, args...) }
func GatewayDebug(format string, args ...any) {
	defaultLogger.log(LevelDebug, "GATEWAY", format, args...)
}
func GatewayError(format string, args ...any) {
	defaultLogger.log(LevelError, "GATEWAY", format, args...)
}
func Config(format string, args ...any) { defaultLogger.log(LevelInfo, "CONFIG", format, args...) }
func System(format string, args ...any) { defaultLogger.log(LevelInfo, "SYSTEM", format, args...) }
func Error(format string, args ...any)  { defaultLogger.log(LevelError, "ERROR", format, args...) }
