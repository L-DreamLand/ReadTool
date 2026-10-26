package server

// Status 是单个监听项的对外视图（`GET /api/health` 的 listeners[] 元素）。
// 字段名与设计规格 §6.3 的 ListenerStatus 一致。
type Status struct {
	// Addr 成功时是实际绑定地址，失败时是配置里的原始 spec（便于用户对照修改）。
	Addr string `json:"addr"`
	OK   bool   `json:"ok"`
	// Err 只在失败时出现。空值被省略，避免正常路径下满是 null。
	Err string `json:"err,omitempty"`
}

// Status 返回所有监听项状态（AC-20-4：失败项必须出现在 health 里，不许静默）。
func (m *Manager) Status() []Status {
	out := make([]Status, 0, len(m.items))
	for _, l := range m.items {
		st := Status{Addr: l.spec, OK: l.ok.Load()}
		if st.OK {
			if a, _ := l.addr.Load().(string); a != "" {
				st.Addr = a
			}
		} else if err := lastErr(l); err != nil {
			st.Err = err.Error()
		}
		out = append(out, st)
	}
	return out
}

// AnyFailed 报告是否存在绑定失败的地址（health 的 degraded 判定之一）。
func (m *Manager) AnyFailed() bool {
	for _, l := range m.items {
		if !l.ok.Load() {
			return true
		}
	}
	return false
}

// AnyOK 报告是否至少有一个地址可用（用于启动期判断）。
func (m *Manager) AnyOK() bool {
	for _, l := range m.items {
		if l.ok.Load() {
			return true
		}
	}
	return false
}
