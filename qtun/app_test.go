package qtun

import (
	"math/rand"
	"testing"
	"time"
)

// TestIsRouteFresh 覆盖新鲜度判断的边界：FetchAndProcessTunPkt 选连接和 CleanRoute
// 清理都依赖它。client 每秒发一次 ping，重启后旧连接停止刷新 lastPing，
// 必须在 routeStaleTimeout 内被判为陈旧（从而不再被选、并被清理）。
func TestIsRouteFresh(t *testing.T) {
	now := time.Now().UnixNano()
	cases := []struct {
		name     string
		lastPing int64
		want     bool
	}{
		{"just refreshed", now, true},
		{"within window", now - int64(time.Second), true},
		{"exactly at threshold", now - int64(routeStaleTimeout), true},
		{"one ns past threshold", now - int64(routeStaleTimeout) - 1, false},
		{"long stale (client restarted)", now - int64(30*time.Second), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isRouteFresh(now, c.lastPing); got != c.want {
				t.Fatalf("isRouteFresh(now, now-%dns) = %v, want %v",
					now-c.lastPing, got, c.want)
			}
		})
	}
}

// TestFreshKeySelection 复刻 FetchAndProcessTunPkt 的候选收集逻辑：一条已刷新的
// 新连接与一条陈旧（模拟 client 重启后残留）的旧连接共存时，只有新连接应入选，
// 因此下行包绝不会被分发到陈旧连接上（这正是方向 2 要根除的静默丢包）。
func TestFreshKeySelection(t *testing.T) {
	now := time.Now().UnixNano()
	const fresh, stale = "10.4.4.2:40000", "10.4.4.2:34507"
	conns := map[string]int64{
		fresh: now - int64(time.Second),       // 新连接，1s 前刚 ping
		stale: now - int64(30*time.Second),    // 旧连接，30s 没刷新
	}

	keys := make([]string, 0, len(conns))
	for k, lastPing := range conns {
		if isRouteFresh(now, lastPing) {
			keys = append(keys, k)
		}
	}

	if len(keys) != 1 || keys[0] != fresh {
		t.Fatalf("candidate keys = %v, want only [%s]", keys, fresh)
	}

	// 随机选多次都不应命中陈旧连接
	for i := 0; i < 100; i++ {
		if keys[rand.Intn(len(keys))] == stale {
			t.Fatal("stale connection was selected for downstream packet")
		}
	}
}
