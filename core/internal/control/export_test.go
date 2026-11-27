package control

import (
	"testing"
	"time"
)

var SyncAccountModels = (*Service).syncAccountModels

func (s *Service) WaitAccountIdleForTest(t testing.TB) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		s.accountMu.Lock()
		idle := !s.accountQuerying && s.accountPending == nil
		s.accountMu.Unlock()
		if idle {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("等待超时: 账号后台工作没有空闲")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
