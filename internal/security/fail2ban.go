package security

import (
	"sync"
	"time"

	"github.com/Eric-Terminal/mcp-streamable-http-gateway/internal/logger"
)

const (
	defaultMaxAttempts   = 10
	defaultWindowSize    = time.Minute
	defaultBanDuration   = time.Hour
	defaultCleanInterval = 5 * time.Minute
)

type ipRecord struct {
	attempts  []time.Time
	bannedAt  time.Time
	banExpiry time.Time
}

type Fail2Ban struct {
	mu          sync.RWMutex
	records     map[string]*ipRecord
	maxAttempts int
	windowSize  time.Duration
	banDuration time.Duration
	stopChan    chan struct{}
}

func NewFail2Ban() *Fail2Ban {
	f := &Fail2Ban{
		records:     make(map[string]*ipRecord),
		maxAttempts: defaultMaxAttempts,
		windowSize:  defaultWindowSize,
		banDuration: defaultBanDuration,
		stopChan:    make(chan struct{}),
	}
	go f.cleanupLoop()
	return f
}

func NewFail2BanWithConfig(maxAttempts int, windowSize, banDuration time.Duration) *Fail2Ban {
	f := &Fail2Ban{
		records:     make(map[string]*ipRecord),
		maxAttempts: maxAttempts,
		windowSize:  windowSize,
		banDuration: banDuration,
		stopChan:    make(chan struct{}),
	}
	go f.cleanupLoop()
	return f
}

func (f *Fail2Ban) IsBanned(ip string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	record, exists := f.records[ip]
	return exists && !record.bannedAt.IsZero() && time.Now().Before(record.banExpiry)
}

func (f *Fail2Ban) RecordFailure(ip string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-f.windowSize)

	record, exists := f.records[ip]
	if !exists {
		record = &ipRecord{attempts: make([]time.Time, 0, f.maxAttempts+1)}
		f.records[ip] = record
	}

	if !record.bannedAt.IsZero() && now.Before(record.banExpiry) {
		return
	}
	if !record.bannedAt.IsZero() && now.After(record.banExpiry) {
		record.attempts = record.attempts[:0]
		record.bannedAt = time.Time{}
		record.banExpiry = time.Time{}
	}

	validAttempts := make([]time.Time, 0, len(record.attempts)+1)
	for _, t := range record.attempts {
		if t.After(windowStart) {
			validAttempts = append(validAttempts, t)
		}
	}
	validAttempts = append(validAttempts, now)
	record.attempts = validAttempts

	if len(record.attempts) > f.maxAttempts {
		record.bannedAt = now
		record.banExpiry = now.Add(f.banDuration)
		record.attempts = record.attempts[:0]
		logger.SecurityWarn("[Fail2Ban] IP已被封禁: ip=%s 封禁时长=%v", ip, f.banDuration)
	}
}

func (f *Fail2Ban) Stop() {
	close(f.stopChan)
}

func (f *Fail2Ban) cleanupLoop() {
	ticker := time.NewTicker(defaultCleanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			f.cleanup()
		case <-f.stopChan:
			return
		}
	}
}

func (f *Fail2Ban) cleanup() {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-f.windowSize)

	for ip, record := range f.records {
		if !record.bannedAt.IsZero() && now.After(record.banExpiry) {
			delete(f.records, ip)
			continue
		}
		if record.bannedAt.IsZero() && !hasValidAttempts(record.attempts, windowStart) {
			delete(f.records, ip)
		}
	}
}

func hasValidAttempts(attempts []time.Time, windowStart time.Time) bool {
	for _, t := range attempts {
		if t.After(windowStart) {
			return true
		}
	}
	return false
}
