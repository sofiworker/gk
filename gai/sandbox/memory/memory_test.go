package memory_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	s "github.com/sofiworker/gk/gai/sandbox"
	"github.com/sofiworker/gk/gai/sandbox/memory"
)

var ctx = context.Background()

func box(t *testing.T, opts ...memory.Option) *memory.Sandbox {
	t.Helper()
	b, err := memory.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func write(t *testing.T, b *memory.Sandbox, p, value string) s.Result {
	t.Helper()
	r, err := b.WriteFile(ctx, s.Call{}, p, []byte(value))
	must(t, err)
	if !r.LocalApplied {
		t.Fatal(r)
	}
	return r
}
func read(t *testing.T, b *memory.Sandbox, p string) string {
	t.Helper()
	v, err := b.ReadFile(ctx, s.Call{}, p)
	must(t, err)
	return string(v)
}
func resource(target s.Target, mode s.SyncMode) s.Resource {
	return s.Resource{Binding: s.Binding{ID: "project", Path: "/custom", Access: s.ReadWrite, Mode: mode, Target: target}, Tree: s.Tree{".": {Dir: true}, "a": {Data: []byte("base")}}}
}
func TestFilesSnapshotsAndAudit(t *testing.T) {
	b := box(t)
	other := box(t)
	entries, err := b.ReadDir(ctx, s.Call{}, "/")
	must(t, err)
	if len(entries) != 0 {
		t.Fatal(entries)
	}
	_, err = b.Mkdir(ctx, s.Call{}, "/arbitrary")
	must(t, err)
	must(t, b.SetDir(ctx, "/arbitrary"))
	r := write(t, b, "file", "first")
	snap, err := b.Snapshot(ctx)
	must(t, err)
	write(t, b, "file", "second")
	_, err = b.Rename(ctx, s.Call{}, "file", "moved")
	must(t, err)
	_, err = b.Remove(ctx, s.Call{}, "moved")
	must(t, err)
	_, err = b.Restore(ctx, snap.ID)
	must(t, err)
	if read(t, b, "file") != "first" {
		t.Fatal("snapshot lost content")
	}
	if _, err = other.ReadFile(ctx, s.Call{}, "/arbitrary/file"); !errors.Is(err, s.ErrNotExist) {
		t.Fatal(err)
	}
	events, err := b.Events(ctx, 0, 100)
	must(t, err)
	var first string
	for _, e := range events {
		if e.OperationID == r.OperationID {
			first = e.AfterContent
		}
	}
	data, err := b.Content(ctx, first)
	must(t, err)
	if string(data) != "first" {
		t.Fatal(string(data))
	}
	data[0] = 'X'
	if read(t, b, "file") != "first" {
		t.Fatal("caller mutated state")
	}
	list, err := b.Snapshots(ctx)
	must(t, err)
	if len(list) != 1 {
		t.Fatal(list)
	}
	must(t, b.Release(ctx, snap.ID))
	got, err := b.Stat(ctx, s.Call{}, "file")
	must(t, err)
	if got.Size != 5 || got.Dir {
		t.Fatal(got)
	}
}
func TestPermissionsPathsAndIdempotency(t *testing.T) {
	b := box(t)
	r := resource(nil, s.SyncNone)
	r.Binding.Access = s.Read
	must(t, b.Provide(ctx, r))
	_, err := b.WriteFile(ctx, s.Call{}, "/custom/a", []byte("no"))
	if !errors.Is(err, s.ErrDenied) {
		t.Fatal(err)
	}
	for _, p := range []string{"../../escape", "/../x", "a\\b", ""} {
		if _, err = b.WriteFile(ctx, s.Call{}, p, nil); !errors.Is(err, s.ErrPath) {
			t.Fatalf("%q: %v", p, err)
		}
	}
	a, err := b.WriteFile(ctx, s.Call{ID: "stable"}, "/value", []byte("one"))
	must(t, err)
	same, err := b.WriteFile(ctx, s.Call{ID: "stable"}, "/value", []byte("one"))
	must(t, err)
	if a != same {
		t.Fatal(a, same)
	}
	_, err = b.WriteFile(ctx, s.Call{ID: "stable"}, "/value", []byte("two"))
	if !errors.Is(err, s.ErrConflict) {
		t.Fatal(err)
	}
	must(t, b.SetAccess(ctx, "project", s.ReadWrite))
	write(t, b, "/custom/a", "allowed")
	_, err = b.Rename(ctx, s.Call{}, "/value", "/custom/value")
	if !errors.Is(err, s.ErrDenied) {
		t.Fatal(err)
	}
	_, err = b.Remove(ctx, s.Call{}, "/custom")
	if !errors.Is(err, s.ErrDenied) {
		t.Fatal(err)
	}
	op, err := b.Operation(ctx, a.OperationID)
	must(t, err)
	if op != a {
		t.Fatal(op)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = b.WriteFile(cancelled, s.Call{}, "/cancelled", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestSnapshotCannotReauthorize(t *testing.T) {
	b := box(t)
	res := resource(nil, s.SyncNone)
	must(t, b.Provide(ctx, res))
	snap, err := b.Snapshot(ctx)
	must(t, err)
	must(t, b.SetAccess(ctx, "project", s.Read))
	if _, err = b.Restore(ctx, snap.ID); !errors.Is(err, s.ErrDenied) {
		t.Fatal(err)
	}
	must(t, b.Revoke(ctx, "project", true))
	must(t, b.Provide(ctx, res))
	if _, err = b.Restore(ctx, snap.ID); !errors.Is(err, s.ErrDenied) {
		t.Fatal(err)
	}
}
func TestConcurrentViewsAndAuditCopies(t *testing.T) {
	b := box(t)
	for _, p := range []string{"/left", "/right"} {
		_, err := b.Mkdir(ctx, s.Call{}, p)
		must(t, err)
	}
	var wg sync.WaitGroup
	for _, dir := range []string{"/left", "/right"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := b.View(s.Call{Dir: dir, SessionID: dir})
			for i := 0; i < 20; i++ {
				_, err := v.WriteFile(ctx, s.Call{Dir: "/", SessionID: "forged"}, fmt.Sprint(i), []byte(dir))
				if err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if read(t, b, "/left/0") != "/left" || read(t, b, "/right/0") != "/right" {
		t.Fatal("views crossed")
	}
	ev, err := b.Events(ctx, 0, 100)
	must(t, err)
	for _, e := range ev {
		if e.Kind == "write" && e.Attribution.SessionID == "forged" {
			t.Fatal(e)
		}
	}
	ev[0].Changes[0].After.ContentID = "forged"
	again, err := b.Events(ctx, 0, 1)
	must(t, err)
	if again[0].Changes[0].After.ContentID == "forged" {
		t.Fatal("audit alias")
	}
}
func TestQuotaAndRetention(t *testing.T) {
	limits := memory.Limits{Bytes: 1 << 20, FileBytes: 4, Entries: 10, Events: 5, Snapshots: 2, Operations: 20, Requests: 2}
	b := box(t, memory.WithLimits(limits))
	_, err := b.WriteFile(ctx, s.Call{}, "/big", []byte("large"))
	if !errors.Is(err, s.ErrQuota) {
		t.Fatal(err)
	}
	write(t, b, "/a", "old")
	write(t, b, "/a", "new")
	events, err := b.Events(ctx, 0, 10)
	must(t, err)
	old := events[0].AfterContent
	_, err = b.Remove(ctx, s.Call{}, "/a")
	must(t, err)
	must(t, b.PruneEvents(ctx, events[0].Sequence))
	if _, err = b.Content(ctx, old); err != nil {
		t.Fatal("later audit still references old content", err)
	}
	must(t, b.PruneEvents(ctx, 1000))
	if _, err = b.Events(ctx, 0, 1); !errors.Is(err, s.ErrCursorExpired) {
		t.Fatal(err)
	}
	if _, err = b.Content(ctx, old); !errors.Is(err, s.ErrNotExist) {
		t.Fatal(err)
	}
}

type target struct {
	mu      sync.Mutex
	calls   []s.Batch
	fail    bool
	partial bool
	unknown bool
}

func (t *target) Apply(_ context.Context, b s.Batch) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, b)
	if t.unknown {
		return 0, s.ErrUnknown
	}
	if t.fail {
		t.fail = false
		if t.partial {
			return 1, s.ErrSyncConflict
		}
		return 0, s.ErrSyncConflict
	}
	return len(b.Changes), nil
}
func TestImmediateSyncFailureAndRecovery(t *testing.T) {
	sink := &target{fail: true}
	b := box(t)
	must(t, b.Provide(ctx, resource(sink, s.SyncImmediate)))
	r, err := b.WriteFile(ctx, s.Call{ID: "write"}, "/custom/a", []byte("new"))
	if !errors.Is(err, s.ErrSyncConflict) || !r.LocalApplied || r.SyncState != s.SyncConflict {
		t.Fatal(r, err)
	}
	if read(t, b, "/custom/a") != "new" {
		t.Fatal("lost local mutation")
	}
	_, err = b.WriteFile(ctx, s.Call{ID: "write"}, "/custom/a", []byte("new"))
	if !errors.Is(err, s.ErrSyncConflict) || len(sink.calls) != 1 {
		t.Fatal(err)
	}
	status, err := b.Sync(ctx, "project")
	must(t, err)
	if status.State != s.SyncClean || len(sink.calls) != 2 {
		t.Fatal(status)
	}
	snap, err := b.Snapshot(ctx)
	must(t, err)
	write(t, b, "/custom/a", "third")
	_, err = b.Restore(ctx, snap.ID)
	must(t, err)
	count := len(sink.calls)
	write(t, b, "/custom/a", "fourth")
	if len(sink.calls) != count {
		t.Fatal("restore did not pause automatic sync")
	}
	must(t, b.ResumeSync(ctx))
	write(t, b, "/custom/a", "fifth")
	if len(sink.calls) != count+1 {
		t.Fatal("resume failed")
	}
}
func TestFrozenBatchPartialProgressAndUnknown(t *testing.T) {
	sink := &target{fail: true, partial: true}
	b := box(t)
	must(t, b.Provide(ctx, resource(sink, s.SyncExplicit)))
	write(t, b, "/custom/a", "one")
	write(t, b, "/custom/b", "two")
	status, err := b.Sync(ctx, "project")
	if !errors.Is(err, s.ErrSyncConflict) || status.Applied != 1 {
		t.Fatal(status, err)
	}
	write(t, b, "/custom/b", "later")
	status, err = b.Sync(ctx, "project")
	must(t, err)
	if string(sink.calls[1].Changes[0].After.Data) != "two" || status.State != s.SyncPending {
		t.Fatal(status)
	}
	status, err = b.Sync(ctx, "project")
	must(t, err)
	if status.State != s.SyncClean {
		t.Fatal(status)
	}
	sink.unknown = true
	write(t, b, "/custom/a", "unknown")
	_, err = b.Sync(ctx, "project")
	if !errors.Is(err, s.ErrUnknown) {
		t.Fatal(err)
	}
	n := len(sink.calls)
	_, err = b.Sync(ctx, "project")
	if !errors.Is(err, s.ErrUnknown) || len(sink.calls) != n {
		t.Fatal("unknown replayed")
	}
	must(t, b.Rebase(ctx, "project", s.Tree{".": {Dir: true}, "a": {Data: []byte("host")}}))
	sink.unknown = false
	_, err = b.Sync(ctx, "project")
	must(t, err)
}
func TestResourceRequestsAndClose(t *testing.T) {
	b := box(t)
	r, err := b.RequestResource(ctx, "request", "documentation", s.Read)
	must(t, err)
	if r.State != s.Requested {
		t.Fatal(r)
	}
	r, err = b.DecideResource(ctx, r.ID, true)
	must(t, err)
	if r.State != s.Approved {
		t.Fatal(r)
	}
	bad := resource(nil, s.SyncNone)
	bad.Tree = nil
	r, err = b.FulfillResource(ctx, r.ID, bad)
	if err == nil || r.State != s.ProvideFailed {
		t.Fatal(r, err)
	}
	r, err = b.FulfillResource(ctx, r.ID, resource(nil, s.SyncNone))
	must(t, err)
	if r.State != s.Available {
		t.Fatal(r)
	}
	queried, err := b.ResourceRequest(ctx, r.ID)
	must(t, err)
	if queried != r {
		t.Fatal(queried)
	}
	bindings, err := b.Bindings(ctx)
	must(t, err)
	if len(bindings) != 1 || bindings[0].Target != nil {
		t.Fatal(bindings)
	}
	must(t, b.Close(ctx))
	if _, err = b.WriteFile(ctx, s.Call{}, "/no", nil); !errors.Is(err, s.ErrClosed) {
		t.Fatal(err)
	}
	_, err = b.Events(ctx, 0, 100)
	must(t, err)
	must(t, b.Destroy(ctx, true))
}

type sender struct {
	started chan struct{}
	release chan struct{}
}

func (sdr *sender) Send(c context.Context, _ s.HTTPRequest) (s.HTTPResponse, error) {
	close(sdr.started)
	if sdr.release != nil {
		<-sdr.release
	} else {
		<-c.Done()
	}
	return s.HTTPResponse{}, c.Err()
}
func TestNetworkCancellationAuditAndRestore(t *testing.T) {
	send := &sender{started: make(chan struct{}), release: make(chan struct{})}
	b := box(t, memory.WithNetwork(send))
	snap, err := b.Snapshot(ctx)
	must(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := b.Request(ctx, s.Call{}, s.HTTPRequest{Method: "GET", URL: "https://example.test/path?secret=hidden"})
		done <- err
	}()
	<-send.started
	_, err = b.Restore(ctx, snap.ID)
	if !errors.Is(err, s.ErrBusy) {
		t.Fatal(err)
	}
	timeout, cancel := context.WithTimeout(ctx, time.Millisecond)
	defer cancel()
	if err = b.Close(timeout); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(send.release)
	<-done
	must(t, b.Close(ctx))
	events, err := b.Events(ctx, 0, 20)
	must(t, err)
	for _, e := range events {
		if e.Path != "" && e.Kind == "http_end" && e.Path != "https://example.test" {
			t.Fatal(e)
		}
	}
}
func TestDeniedNetworkAndOptions(t *testing.T) {
	b := box(t, memory.WithAccess(s.Read))
	_, err := b.WriteFile(ctx, s.Call{}, "/x", nil)
	if !errors.Is(err, s.ErrDenied) {
		t.Fatal(err)
	}
	_, err = b.Request(ctx, s.Call{}, s.HTTPRequest{Method: "GET", URL: "https://example.test"})
	if !errors.Is(err, s.ErrDenied) {
		t.Fatal(err)
	}
	for _, opt := range []memory.Option{memory.WithID(""), memory.WithAccess(99), memory.WithLimits(memory.Limits{}), memory.WithNetwork(nil), nil} {
		if _, err := memory.New(opt); err == nil {
			t.Fatal("invalid option accepted")
		}
	}
}

type successSender struct{}

func (successSender) Send(context.Context, s.HTTPRequest) (s.HTTPResponse, error) {
	return s.HTTPResponse{StatusCode: 200, Body: []byte("response")}, nil
}
func TestViewCapabilitiesDiffAndNetworkPolicy(t *testing.T) {
	b := box(t, memory.WithID("known"))
	if b.ID() != "known" {
		t.Fatal(b.ID())
	}
	view := b.View(s.Call{Dir: "/", TurnID: "turn"})
	_, err := view.Mkdir(ctx, s.Call{}, "/named")
	must(t, err)
	snap, err := b.Snapshot(ctx)
	must(t, err)
	_, err = view.WriteFile(ctx, s.Call{}, "/named/a", []byte("value"))
	must(t, err)
	data, err := view.ReadFile(ctx, s.Call{}, "/named/a")
	must(t, err)
	if string(data) != "value" {
		t.Fatal(string(data))
	}
	info, err := view.Stat(ctx, s.Call{}, "/named/a")
	must(t, err)
	if info.Size != 5 {
		t.Fatal(info)
	}
	entries, err := view.ReadDir(ctx, s.Call{}, "/named")
	must(t, err)
	if len(entries) != 1 {
		t.Fatal(entries)
	}
	second, err := b.Snapshot(ctx)
	must(t, err)
	diff, err := b.Diff(ctx, snap.ID, second.ID)
	must(t, err)
	if len(diff.Changes) != 1 || diff.Changes[0].Before != nil {
		t.Fatal(diff)
	}
	_, err = view.Rename(ctx, s.Call{}, "/named/a", "/named/b")
	must(t, err)
	_, err = view.Remove(ctx, s.Call{}, "/named/b")
	must(t, err)
	diff, err = b.Diff(ctx, snap.ID, "")
	must(t, err)
	if len(diff.Changes) != 0 {
		t.Fatal(diff)
	}
	req, err := view.RequestResource(ctx, "request", "need access", s.Read)
	must(t, err)
	if req.State != s.Requested {
		t.Fatal(req)
	}
	must(t, b.SetNetwork(ctx, successSender{}))
	response, err := view.Request(ctx, s.Call{}, s.HTTPRequest{Method: "GET", URL: "https://example.test/path?token=secret"})
	must(t, err)
	if response.StatusCode != 200 {
		t.Fatal(response)
	}
	must(t, b.SetNetwork(ctx, nil))
	_, err = view.Request(ctx, s.Call{}, s.HTTPRequest{Method: "GET", URL: "https://example.test"})
	if !errors.Is(err, s.ErrDenied) {
		t.Fatal(err)
	}
	state, err := b.State(ctx)
	must(t, err)
	if state.ID != "known" || state.Revision == 0 || state.PolicyVersion != 2 {
		t.Fatal(state)
	}
	must(t, b.Close(ctx))
	state, err = b.State(ctx)
	must(t, err)
	if !state.Closed {
		t.Fatal(state)
	}
	must(t, b.Destroy(ctx, false))
	if _, err = b.State(ctx); !errors.Is(err, s.ErrClosed) {
		t.Fatal(err)
	}
}
func TestSyncToSnapshotAndImmediateQueue(t *testing.T) {
	sink := &target{}
	b := box(t)
	must(t, b.Provide(ctx, resource(sink, s.SyncExplicit)))
	write(t, b, "/custom/a", "selected")
	snap, err := b.Snapshot(ctx)
	must(t, err)
	write(t, b, "/custom/a", "later")
	status, err := b.SyncTo(ctx, "project", snap.ID)
	must(t, err)
	if status.State != s.SyncPending || string(sink.calls[0].Changes[0].After.Data) != "selected" || read(t, b, "/custom/a") != "later" {
		t.Fatal(status)
	}
	got, err := b.SyncStatus(ctx, "project")
	must(t, err)
	if got != status {
		t.Fatal(got)
	}
	_, err = b.Sync(ctx, "project")
	must(t, err)
	if string(sink.calls[1].Changes[0].After.Data) != "later" {
		t.Fatal(sink.calls)
	}
	immediate := &target{fail: true}
	other := box(t)
	must(t, other.Provide(ctx, resource(immediate, s.SyncImmediate)))
	_, err = other.WriteFile(ctx, s.Call{}, "/custom/a", []byte("failed"))
	if !errors.Is(err, s.ErrSyncConflict) {
		t.Fatal(err)
	}
	result := write(t, other, "/custom/a", "newer")
	if result.SyncState != s.SyncClean || len(immediate.calls) != 3 {
		t.Fatal(result, len(immediate.calls))
	}
}
func TestCloseDoesNotDiscardPendingAndBindingConflicts(t *testing.T) {
	b := box(t)
	sink := &target{}
	must(t, b.Provide(ctx, resource(sink, s.SyncExplicit)))
	res := resource(nil, s.SyncNone)
	res.Binding.ID = "nested"
	res.Binding.Path = "/custom/child"
	if err := b.Provide(ctx, res); !errors.Is(err, s.ErrConflict) {
		t.Fatal(err)
	}
	write(t, b, "/custom/a", "pending")
	if err := b.Revoke(ctx, "project", false); !errors.Is(err, s.ErrConflict) {
		t.Fatal(err)
	}
	must(t, b.Close(ctx))
	if len(sink.calls) != 0 {
		t.Fatal("close synchronized")
	}
	if err := b.Destroy(ctx, false); !errors.Is(err, s.ErrConflict) {
		t.Fatal(err)
	}
	must(t, b.Destroy(ctx, true))
}
func TestAuditCapacityAndDeniedWriteDoNotMutate(t *testing.T) {
	limits := memory.Limits{Bytes: 1 << 20, FileBytes: 1024, Entries: 10, Events: 2, Snapshots: 2, Operations: 10, Requests: 2}
	b := box(t, memory.WithLimits(limits))
	write(t, b, "/a", "kept")
	if _, err := b.WriteFile(ctx, s.Call{}, "/a", []byte("lost")); !errors.Is(err, s.ErrQuota) {
		t.Fatal(err)
	}
	must(t, b.PruneEvents(ctx, 100))
	if read(t, b, "/a") != "kept" {
		t.Fatal("quota mutation escaped")
	}
}
