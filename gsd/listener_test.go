package gsd

import (
	"reflect"
	"sync"
	"testing"
)

// recordingListener 记录收到的所有快照，供断言。
// recordingListener records every received snapshot for assertions.
type recordingListener struct {
	mu        sync.Mutex
	snapshots [][]ServiceInfo
}

func (r *recordingListener) Update(instances []ServiceInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshots = append(r.snapshots, append([]ServiceInfo(nil), instances...))
}

func (r *recordingListener) all() [][]ServiceInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]ServiceInfo(nil), r.snapshots...)
}

func svc(addr string) ServiceInfo {
	return ServiceInfo{Name: "api", Address: addr, Port: 8080}
}

func TestDedupListenerSkipsIdenticalSnapshots(t *testing.T) {
	rec := &recordingListener{}
	d := NewDedupListener(rec)

	d.Update([]ServiceInfo{svc("10.0.0.1"), svc("10.0.0.2")})
	d.Update([]ServiceInfo{svc("10.0.0.2"), svc("10.0.0.1")}) // 乱序，签名相同 → 丢弃
	d.Update([]ServiceInfo{svc("10.0.0.1"), svc("10.0.0.2")}) // 重复 → 丢弃
	d.Update([]ServiceInfo{svc("10.0.0.1")})                  // 变化 → 转发

	got := rec.all()
	if len(got) != 2 {
		t.Fatalf("forwarded %d snapshots, want 2", len(got))
	}
	if !reflect.DeepEqual(got[0], []ServiceInfo{svc("10.0.0.1"), svc("10.0.0.2")}) {
		t.Fatalf("first snapshot = %+v, want original order", got[0])
	}
	if !reflect.DeepEqual(got[1], []ServiceInfo{svc("10.0.0.1")}) {
		t.Fatalf("second snapshot = %+v, want updated", got[1])
	}
}

func TestDedupListenerForwardsEmptyOnce(t *testing.T) {
	rec := &recordingListener{}
	d := NewDedupListener(rec)

	d.Update([]ServiceInfo{svc("10.0.0.1")})
	d.Update(nil) // 空快照，首次转发
	d.Update(nil) // 空快照，重复 → 丢弃

	if got := rec.all(); len(got) != 2 {
		t.Fatalf("forwarded %d snapshots, want 2", len(got))
	}
}

func TestDedupListenerFirstUpdateAlwaysForwarded(t *testing.T) {
	rec := &recordingListener{}
	d := NewDedupListener(rec)

	d.Update([]ServiceInfo{svc("10.0.0.1")}) // 首次即使与"空"相同也应转发
	d.Update([]ServiceInfo{svc("10.0.0.1")}) // 第二次才去重

	if got := rec.all(); len(got) != 1 {
		t.Fatalf("forwarded %d snapshots, want 1", len(got))
	}
}

func TestFallbackListenerPrimaryWins(t *testing.T) {
	rec := &recordingListener{}
	primary, backup := NewFallbackListener(rec)

	// 仅主源更新：等待备源初始化，不发布
	primary.Update([]ServiceInfo{svc("10.0.0.1")})
	if got := rec.all(); len(got) != 0 {
		t.Fatalf("published before backup initialized: %d snapshots", len(got))
	}

	backup.Update([]ServiceInfo{svc("10.0.0.9")})
	// 两源就绪后，主源非空 → 发布主源
	got := rec.all()
	if len(got) != 1 {
		t.Fatalf("published %d snapshots, want 1", len(got))
	}
	if !reflect.DeepEqual(got[0], []ServiceInfo{svc("10.0.0.1")}) {
		t.Fatalf("snapshot = %+v, want primary", got[0])
	}
}

func TestFallbackListenerFallsBackWhenPrimaryEmpty(t *testing.T) {
	rec := &recordingListener{}
	primary, backup := NewFallbackListener(rec)

	primary.Update(nil) // 主源空
	backup.Update([]ServiceInfo{svc("10.0.0.9")})

	got := rec.all()
	if len(got) != 1 {
		t.Fatalf("published %d snapshots, want 1", len(got))
	}
	if !reflect.DeepEqual(got[0], []ServiceInfo{svc("10.0.0.9")}) {
		t.Fatalf("snapshot = %+v, want backup", got[0])
	}
}

func TestFallbackListenerEmptyOnlyWhenBothEmpty(t *testing.T) {
	rec := &recordingListener{}
	primary, backup := NewFallbackListener(rec)

	primary.Update(nil)
	backup.Update(nil)

	got := rec.all()
	if len(got) != 1 {
		t.Fatalf("published %d snapshots, want 1", len(got))
	}
	if len(got[0]) != 0 {
		t.Fatalf("snapshot = %+v, want empty", got[0])
	}
}

func TestFallbackListenerSwitchesBackOnPrimaryUpdate(t *testing.T) {
	rec := &recordingListener{}
	primary, backup := NewFallbackListener(rec)

	primary.Update([]ServiceInfo{svc("10.0.0.1")})
	backup.Update([]ServiceInfo{svc("10.0.0.9")})
	// 主源变空 → 回退备源
	primary.Update(nil)

	got := rec.all()
	if len(got) != 2 {
		t.Fatalf("published %d snapshots, want 2", len(got))
	}
	if !reflect.DeepEqual(got[1], []ServiceInfo{svc("10.0.0.9")}) {
		t.Fatalf("second snapshot = %+v, want backup", got[1])
	}
	// 主源恢复 → 切回主源
	primary.Update([]ServiceInfo{svc("10.0.0.2")})
	got = rec.all()
	if len(got) != 3 {
		t.Fatalf("published %d snapshots, want 3", len(got))
	}
	if !reflect.DeepEqual(got[2], []ServiceInfo{svc("10.0.0.2")}) {
		t.Fatalf("third snapshot = %+v, want primary", got[2])
	}
}

func TestFallbackListenerConcurrentSafe(t *testing.T) {
	rec := &recordingListener{}
	primary, backup := NewFallbackListener(rec)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			primary.Update([]ServiceInfo{svc("10.0.0.1")})
		}(i)
		go func(n int) {
			defer wg.Done()
			backup.Update(nil)
		}(i)
	}
	wg.Wait()

	// 并发下不 panic、无数据竞争（-race 检测）。
	// concurrent updates must not panic or race (checked under -race).
	if got := rec.all(); len(got) < 1 {
		t.Fatalf("expected at least one published snapshot, got %d", len(got))
	}
}
