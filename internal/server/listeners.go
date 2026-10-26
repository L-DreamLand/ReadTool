// Package server 负责"监听多个地址并在失败后自愈"（FR-20 / 专项 A）。
package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// listener 是单个监听项的运行时状态。
// 每个 listener 自带锁：一个地址的病不能拖累其他地址。
type listener struct {
	spec string

	addr atomic.Value // string：实际绑定到的地址（主机名解析后可能不同于 spec）
	ok   atomic.Bool

	errMu   sync.Mutex
	lastErr error

	ln net.Listener
	mu sync.Mutex
}

// Manager 管理同一 *http.Server 上的多个监听地址。
type Manager struct {
	items  []*listener
	srv    *http.Server
	retry  time.Duration
	log    *slog.Logger
	closed atomic.Bool
	done   chan struct{}
}

// NewManager 构造管理器。retry <= 0 表示关闭自愈。
func NewManager(specs []string, retry time.Duration, srv *http.Server, log *slog.Logger) *Manager {
	items := make([]*listener, 0, len(specs))
	for _, s := range specs {
		items = append(items, &listener{spec: s})
	}
	return &Manager{
		items: items,
		srv:   srv,
		retry: retry,
		log:   log,
		done:  make(chan struct{}),
	}
}

// Start 逐个绑定。设计要点（技术实现路径 专项 A.2）：
//   - 部分失败**不返回错误**：EasyTier 虚拟网卡晚就绪是常态而非异常（AC-20-2）
//   - 只有全部失败才算启动失败，并在错误里带上首个原因
func (m *Manager) Start() error {
	var firstErr error
	okCount := 0
	for _, l := range m.items {
		if err := m.bind(l); err != nil {
			m.log.Error("listen failed", "spec", l.spec, "err", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		okCount++
	}
	if okCount == 0 {
		return fmt.Errorf("全部监听地址均绑定失败，首个错误：%w", firstErr)
	}
	go m.retryLoop()
	return nil
}

// bind 绑定单个地址。
// 关键：net.Listen 用原始 spec 而不是预解析出的 IP——每次重绑都重新解析 DNS，
// 这样用户可以用主机名扛住虚拟 IP 漂移（FR-20 规则 4）。
func (m *Manager) bind(l *listener) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.ln != nil {
		_ = l.ln.Close()
		l.ln = nil
	}

	ln, err := net.Listen("tcp", l.spec)
	if err != nil {
		l.ok.Store(false)
		l.errMu.Lock()
		l.lastErr = err
		l.errMu.Unlock()
		return err
	}

	l.ln = ln
	addr := ln.Addr().String()
	l.addr.Store(addr)
	l.errMu.Lock()
	l.lastErr = nil
	l.errMu.Unlock()
	l.ok.Store(true)

	go func() {
		serveErr := m.srv.Serve(ln)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && !m.closed.Load() {
			// 网卡被拔掉 / 隧道断开时走到这里：标记失败，交给重试循环救回（R-11）
			m.log.Error("listener stopped", "addr", addr, "err", serveErr)
			l.ok.Store(false)
			l.errMu.Lock()
			l.lastErr = serveErr
			l.errMu.Unlock()
		}
	}()

	m.log.Info("listening", "spec", l.spec, "addr", addr)
	return nil
}

func (m *Manager) retryLoop() {
	if m.retry <= 0 {
		return
	}
	ticker := time.NewTicker(m.retry)
	defer ticker.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			for _, l := range m.items {
				if l.ok.Load() {
					continue
				}
				if err := m.bind(l); err != nil {
					// 预期之内（网卡还没出现），用 Debug 避免刷屏
					m.log.Debug("rebind failed", "spec", l.spec, "err", err)
					continue
				}
				m.log.Info("late bind succeeded", "spec", l.spec, "addr", l.addr.Load())
			}
		}
	}
}

// Close 关闭所有监听并停止自愈。由 cmd 在关停序列里调用。
func (m *Manager) Close() error {
	if !m.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(m.done)

	var firstErr error
	for _, l := range m.items {
		l.mu.Lock()
		if l.ln != nil {
			if err := l.ln.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
			l.ln = nil
		}
		l.mu.Unlock()
	}
	return firstErr
}

// Specs 返回配置的监听项（不含绑定结果）。
func (m *Manager) Specs() []string {
	out := make([]string, 0, len(m.items))
	for _, l := range m.items {
		out = append(out, l.spec)
	}
	return out
}

func lastErr(l *listener) error {
	l.errMu.Lock()
	defer l.errMu.Unlock()
	return l.lastErr
}
