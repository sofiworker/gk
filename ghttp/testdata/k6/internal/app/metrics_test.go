package app

import (
	"sync"
	"testing"
)

func TestRuntimeMetricsRequestsConnectionsAndReset(t *testing.T) {
	metrics := NewRuntimeMetrics()
	finishFirst := metrics.BeginRequest()
	finishSecond := metrics.BeginRequest()

	snapshot := metrics.Snapshot()
	if snapshot.ActiveRequests != 2 || snapshot.PeakRequests != 2 || snapshot.TotalRequests != 2 {
		t.Fatalf("Snapshot() during requests = %+v", snapshot)
	}

	finishFirst(200, 10, 20)
	finishSecond(503, 30, 40)
	metrics.OpenSSE()
	metrics.OpenWebSocket()

	snapshot = metrics.Snapshot()
	if snapshot.ActiveRequests != 0 || snapshot.PeakRequests != 2 || snapshot.TotalRequests != 2 {
		t.Fatalf("Snapshot() request counters = %+v", snapshot)
	}
	if snapshot.RequestBytes != 40 || snapshot.ResponseBytes != 60 {
		t.Fatalf("Snapshot() bytes = (%d, %d), want (40, 60)", snapshot.RequestBytes, snapshot.ResponseBytes)
	}
	if snapshot.Statuses["200"] != 1 || snapshot.Statuses["503"] != 1 {
		t.Fatalf("Snapshot() statuses = %v", snapshot.Statuses)
	}
	if snapshot.SSEConnections != 1 || snapshot.WSConnections != 1 {
		t.Fatalf("Snapshot() connections = (%d, %d), want (1, 1)", snapshot.SSEConnections, snapshot.WSConnections)
	}
	if snapshot.Goroutines < 1 || snapshot.HeapAlloc == 0 || snapshot.HeapInUse == 0 {
		t.Fatalf("Snapshot() runtime fields = %+v", snapshot)
	}

	metrics.CloseSSE()
	metrics.CloseSSE()
	metrics.CloseWebSocket()
	metrics.CloseWebSocket()
	metrics.Reset()
	snapshot = metrics.Snapshot()
	if snapshot.ActiveRequests != 0 || snapshot.PeakRequests != 0 || snapshot.TotalRequests != 0 ||
		snapshot.SSEConnections != 0 || snapshot.WSConnections != 0 || snapshot.RequestBytes != 0 ||
		snapshot.ResponseBytes != 0 || len(snapshot.Statuses) != 0 {
		t.Fatalf("Snapshot() after Reset() = %+v", snapshot)
	}
}

func TestRuntimeMetricsConcurrentRequests(t *testing.T) {
	metrics := NewRuntimeMetrics()
	const workers = 32
	var wg sync.WaitGroup
	started := make(chan struct{}, workers)
	release := make(chan struct{})
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			finish := metrics.BeginRequest()
			started <- struct{}{}
			<-release
			finish(201, 3, 7)
		}()
	}
	for worker := 0; worker < workers; worker++ {
		<-started
	}
	if snapshot := metrics.Snapshot(); snapshot.ActiveRequests != workers || snapshot.PeakRequests != workers {
		t.Fatalf("Snapshot() while blocked = %+v, want active/peak %d", snapshot, workers)
	}
	close(release)
	wg.Wait()

	snapshot := metrics.Snapshot()
	want := uint64(workers)
	if snapshot.ActiveRequests != 0 || snapshot.TotalRequests != want || snapshot.Statuses["201"] != want {
		t.Fatalf("Snapshot() counters = %+v, want total/status %d", snapshot, want)
	}
	if snapshot.RequestBytes != want*3 || snapshot.ResponseBytes != want*7 || snapshot.PeakRequests != workers {
		t.Fatalf("Snapshot() bytes/peak = %+v", snapshot)
	}
}

func TestRuntimeMetricsResetIgnoresPreviousGenerationCompletion(t *testing.T) {
	metrics := NewRuntimeMetrics()
	oldFinish := metrics.BeginRequest()
	metrics.Reset()
	newFinish := metrics.BeginRequest()

	oldFinish(500, 100, 200)
	snapshot := metrics.Snapshot()
	if snapshot.ActiveRequests != 1 || snapshot.TotalRequests != 1 || snapshot.PeakRequests != 1 {
		t.Fatalf("Snapshot() after old completion = %+v, want new request counters unchanged", snapshot)
	}
	if snapshot.RequestBytes != 0 || snapshot.ResponseBytes != 0 || len(snapshot.Statuses) != 0 {
		t.Fatalf("Snapshot() after old completion contains old generation data: %+v", snapshot)
	}

	newFinish(204, 3, 4)
	snapshot = metrics.Snapshot()
	if snapshot.ActiveRequests != 0 || snapshot.Statuses["204"] != 1 || snapshot.RequestBytes != 3 || snapshot.ResponseBytes != 4 {
		t.Fatalf("Snapshot() after new completion = %+v", snapshot)
	}
}

func TestRuntimeMetricsResetIgnoresPreviousGenerationStreamClose(t *testing.T) {
	metrics := NewRuntimeMetrics()
	closeOldSSE := metrics.OpenSSE()
	closeOldWebSocket := metrics.OpenWebSocket()
	metrics.Reset()
	closeNewSSE := metrics.OpenSSE()
	closeNewWebSocket := metrics.OpenWebSocket()

	closeOldSSE()
	closeOldWebSocket()
	if snapshot := metrics.Snapshot(); snapshot.SSEConnections != 1 || snapshot.WSConnections != 1 {
		t.Fatalf("Snapshot() after old stream close = %+v, want new streams unchanged", snapshot)
	}

	closeNewSSE()
	closeNewWebSocket()
	if snapshot := metrics.Snapshot(); snapshot.SSEConnections != 0 || snapshot.WSConnections != 0 {
		t.Fatalf("Snapshot() after new stream close = %+v, want zero streams", snapshot)
	}
}

func TestRuntimeMetricsSnapshotStatusesAreIndependent(t *testing.T) {
	metrics := NewRuntimeMetrics()
	finish := metrics.BeginRequest()
	finish(200, 0, 0)

	first := metrics.Snapshot()
	first.Statuses["200"] = 99
	first.Statuses["500"] = 1
	second := metrics.Snapshot()
	if second.Statuses["200"] != 1 || second.Statuses["500"] != 0 {
		t.Fatalf("Snapshot() statuses share mutable state: %v", second.Statuses)
	}
}

func TestRuntimeMetricsConcurrentResetIsolatesPreviousGeneration(t *testing.T) {
	const rounds = 25
	const oldRequests = 16
	const newRequests = 8

	for round := 0; round < rounds; round++ {
		metrics := NewRuntimeMetrics()
		oldFinishes := make([]func(int, uint64, uint64), oldRequests)
		for request := range oldFinishes {
			oldFinishes[request] = metrics.BeginRequest()
		}

		compete := make(chan struct{})
		delayed := make(chan struct{})
		resetDone := make(chan struct{})
		var oldWG sync.WaitGroup
		for request, finish := range oldFinishes {
			oldWG.Add(1)
			go func(request int, finish func(int, uint64, uint64)) {
				defer oldWG.Done()
				if request < oldRequests/2 {
					<-compete
				} else {
					<-delayed
				}
				finish(500, 100, 200)
			}(request, finish)
		}
		go func() {
			<-compete
			metrics.Reset()
			close(resetDone)
		}()

		close(compete)
		<-resetDone
		newFinishes := make([]func(int, uint64, uint64), newRequests)
		for request := range newFinishes {
			newFinishes[request] = metrics.BeginRequest()
		}
		close(delayed)
		for _, finish := range newFinishes {
			finish(201, 3, 7)
		}
		oldWG.Wait()

		snapshot := metrics.Snapshot()
		if snapshot.ActiveRequests != 0 || snapshot.TotalRequests != newRequests || snapshot.PeakRequests != newRequests {
			t.Fatalf("round %d: Snapshot() counters = %+v, want only %d new requests", round, snapshot, newRequests)
		}
		if snapshot.Statuses["201"] != newRequests || snapshot.Statuses["500"] != 0 {
			t.Fatalf("round %d: Snapshot() statuses = %v, want only new generation", round, snapshot.Statuses)
		}
		if snapshot.RequestBytes != newRequests*3 || snapshot.ResponseBytes != newRequests*7 {
			t.Fatalf("round %d: Snapshot() bytes = (%d, %d), want (%d, %d)", round,
				snapshot.RequestBytes, snapshot.ResponseBytes, newRequests*3, newRequests*7)
		}
	}
}
