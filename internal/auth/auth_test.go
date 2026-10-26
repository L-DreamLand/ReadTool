package auth

import (
	"strings"
	"testing"
	"time"
)

func TestHashVerify(t *testing.T) {
	h, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "argon2id$v=19$") {
		t.Fatalf("哈希格式不对：%s", h)
	}
	if !Verify(h, "correct horse battery staple") {
		t.Error("正确口令应通过")
	}
	if Verify(h, "wrong") {
		t.Error("错误口令必须失败")
	}
	if Verify("垃圾数据", "whatever") {
		t.Error("无效哈希必须失败，而不是 panic 或通过")
	}
	// 同一口令两次哈希必须不同（有盐）
	h2, _ := Hash("correct horse battery staple")
	if h == h2 {
		t.Error("两次哈希相同说明没有盐")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	s := &Session{Secret: []byte("0123456789abcdef0123456789abcdef"), Days: 30}
	now := time.Now()
	token, exp := s.Issue("device-1", now)
	if exp <= now.UnixMilli() {
		t.Fatal("到期时间应在未来")
	}
	dev, gotExp, err := s.Parse(token, now)
	if err != nil {
		t.Fatalf("应解析成功：%v", err)
	}
	if dev != "device-1" || gotExp != exp {
		t.Fatalf("载荷不一致：dev=%s exp=%d want %d", dev, gotExp, exp)
	}
}

func TestSessionRejectsTampering(t *testing.T) {
	s := &Session{Secret: []byte("secret-key-0000000000000000000000"), Days: 30}
	now := time.Now()
	token, _ := s.Issue("device-1", now)

	if _, _, err := s.Parse(token+"x", now); err == nil {
		t.Error("被改的签名必须失败")
	}
	if _, _, err := s.Parse("garbage", now); err == nil {
		t.Error("无分隔符的 token 必须失败")
	}
	// 换密钥（logout-all）后旧 token 全部失效
	s2 := &Session{Secret: []byte("another-key-11111111111111111111"), Days: 30}
	if _, _, err := s2.Parse(token, now); err == nil {
		t.Error("轮换密钥后旧会话必须失效")
	}
	// 过期
	old := now.Add(-31 * 24 * time.Hour)
	token2, _ := s.Issue("device-1", old)
	if _, _, err := s.Parse(token2, now); err == nil {
		t.Error("过期会话必须失败")
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(3, time.Minute, time.Minute)
	now := time.Now()
	key := "1.2.3.4"

	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow(key, now); !ok {
			t.Fatalf("第 %d 次就被拦住了", i+1)
		}
		l.Fail(key, now)
	}
	ok, wait := l.Allow(key, now)
	if ok {
		t.Fatal("达到上限后必须锁住")
	}
	if wait <= 0 {
		t.Fatal("锁定应返回剩余时间")
	}
	// 锁到期后放行
	if ok, _ := l.Allow(key, now.Add(2*time.Minute)); !ok {
		t.Error("锁到期后应放行")
	}
	// 其他 IP 不受影响
	if ok, _ := l.Allow("5.6.7.8", now); !ok {
		t.Error("限流不应影响其他 IP")
	}
	// 成功登录后清零
	l.Fail("9.9.9.9", now)
	l.Reset("9.9.9.9")
	if ok, _ := l.Allow("9.9.9.9", now); !ok {
		t.Error("Reset 后应放行")
	}
}
