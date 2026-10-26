package auth

import (
	"sync"
	"time"
)

// Limiter 是内存里的登录失败限流：单 IP 在窗口期内失败次数到上限后锁一段时间。
// 不做持久化——重启清零，代价可接受（攻击者也没法靠重启服务绕过口令）。
type Limiter struct {
	MaxAttempts int
	Window      time.Duration
	Lock        time.Duration

	mu   sync.Mutex
	recs map[string]*rec
}

type rec struct {
	fails   int
	firstAt time.Time
	locked  time.Time
}

// NewLimiter 构造限流器。
func NewLimiter(maxAttempts int, window, lock time.Duration) *Limiter {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if window <= 0 {
		window = time.Minute
	}
	if lock <= 0 {
		lock = time.Minute
	}
	return &Limiter{MaxAttempts: maxAttempts, Window: window, Lock: lock, recs: map[string]*rec{}}
}

// Allow 判断是否允许尝试；被锁时返回剩余时间。
func (l *Limiter) Allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.recs[key]
	if !ok {
		return true, 0
	}
	if !r.locked.IsZero() {
		if now.Before(r.locked) {
			return false, r.locked.Sub(now)
		}
		// 锁到期，清空重新计数
		delete(l.recs, key)
		return true, 0
	}
	if now.Sub(r.firstAt) > l.Window {
		delete(l.recs, key)
	}
	return true, 0
}

// Fail 记一次失败；达到上限则加锁。
func (l *Limiter) Fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.recs[key]
	if !ok || now.Sub(r.firstAt) > l.Window {
		r = &rec{firstAt: now}
		l.recs[key] = r
	}
	r.fails++
	if r.fails >= l.MaxAttempts {
		r.locked = now.Add(l.Lock)
		r.fails = 0
	}
}

// Reset 登录成功后清掉记录。
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.recs, key)
}

// Sweep 清理超过窗口期的记录，避免长期运行后 map 无限增长。
func (l *Limiter) Sweep(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, r := range l.recs {
		if r.locked.IsZero() && now.Sub(r.firstAt) > l.Window {
			delete(l.recs, k)
		}
	}
}
