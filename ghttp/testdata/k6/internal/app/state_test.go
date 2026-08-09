package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestStateCRUDChain(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)

	created := stateRequest(t, handler, http.MethodPost, "/state/team/items", `{"id":"b","name":"beta"}`, nil)
	assertStateResponse(t, created, http.StatusCreated)
	if created.Header().Get("Location") != "/state/team/items/b" || created.Header().Get("ETag") != `"v1"` {
		t.Fatalf("create headers = Location %q ETag %q", created.Header().Get("Location"), created.Header().Get("ETag"))
	}
	stateRequest(t, handler, http.MethodPost, "/state/team/items", `{"id":"a","name":"alpha"}`, nil)

	got := stateRequest(t, handler, http.MethodGet, "/state/team/items/b", "", nil)
	assertStateResponse(t, got, http.StatusOK)
	if got.Header().Get("ETag") != `"v1"` || decodeStateItem(t, got).Name != "beta" {
		t.Fatalf("get = ETag %q body %s", got.Header().Get("ETag"), got.Body.String())
	}

	updated := stateRequest(t, handler, http.MethodPut, "/state/team/items/b", `{"name":"updated"}`, map[string]string{"If-Match": `"v1"`})
	assertStateResponse(t, updated, http.StatusOK)
	if updated.Header().Get("ETag") != `"v2"` || decodeStateItem(t, updated).Version != 2 {
		t.Fatalf("update = ETag %q body %s", updated.Header().Get("ETag"), updated.Body.String())
	}

	listed := stateRequest(t, handler, http.MethodGet, "/state/team/items", "", nil)
	assertStateResponse(t, listed, http.StatusOK)
	var items []Item
	if err := json.Unmarshal(listed.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if ids := []string{items[0].ID, items[1].ID}; !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Fatalf("list IDs = %v", ids)
	}

	deleted := stateRequest(t, handler, http.MethodDelete, "/state/team/items/b", "", nil)
	if deleted.Code != http.StatusNoContent || deleted.Body.Len() != 0 {
		t.Fatalf("delete = %d body %q", deleted.Code, deleted.Body.String())
	}
	assertStateError(t, stateRequest(t, handler, http.MethodGet, "/state/team/items/b", "", nil), http.StatusNotFound)
	assertStateError(t, stateRequest(t, handler, http.MethodDelete, "/state/team/items/b", "", nil), http.StatusNotFound)
	assertStateError(t, stateRequest(t, handler, http.MethodPost, "/state/team/items", `{"id":"a","name":"duplicate"}`, nil), http.StatusConflict)
}

func TestETagPreconditions(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	stateRequest(t, handler, http.MethodPost, "/state/n/items", `{"id":"x","name":"one"}`, nil)

	assertStateError(t, stateRequest(t, handler, http.MethodPut, "/state/n/items/x", `{"name":"two"}`, nil), http.StatusPreconditionRequired)
	assertStateError(t, stateRequest(t, handler, http.MethodPut, "/state/n/items/x", `{"name":"two"}`, map[string]string{"If-Match": `"v9"`}), http.StatusPreconditionFailed)
	assertStateError(t, stateRequest(t, handler, http.MethodPut, "/state/n/items/x", `{"name":"two"}`, map[string]string{"If-Match": `W/"v1"`}), http.StatusPreconditionFailed)
	assertStateError(t, stateRequest(t, handler, http.MethodPut, "/state/n/items/x", `{"name":"two"}`, map[string]string{"If-Match": `"v1", "v2"`}), http.StatusPreconditionFailed)
	assertStateError(t, stateRequest(t, handler, http.MethodPut, "/state/n/items/missing", `{"name":"two"}`, map[string]string{"If-Match": `"v1"`}), http.StatusNotFound)
}

func TestIdempotencySequentialConcurrentAndNamespaceIsolation(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	headers := map[string]string{"Idempotency-Key": "same-key"}
	first := stateRequest(t, handler, http.MethodPost, "/state/a/items", `{"id":"x","name":"one"}`, headers)
	replay := stateRequest(t, handler, http.MethodPost, "/state/a/items", `{"id":"x","name":"one"}`, headers)
	assertStateResponse(t, first, http.StatusCreated)
	assertStateResponse(t, replay, http.StatusCreated)
	if first.Body.String() != replay.Body.String() || first.Header().Get("Location") != replay.Header().Get("Location") || first.Header().Get("ETag") != replay.Header().Get("ETag") {
		t.Fatalf("replay differs: first=%s replay=%s", first.Body.String(), replay.Body.String())
	}
	assertStateError(t, stateRequest(t, handler, http.MethodPost, "/state/a/items", `{"id":"y","name":"other"}`, headers), http.StatusConflict)
	assertStateResponse(t, stateRequest(t, handler, http.MethodPost, "/state/b/items", `{"id":"x","name":"one"}`, headers), http.StatusCreated)

	const workers = 32
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses <- stateRequest(t, handler, http.MethodPost, "/state/concurrent/items", `{"id":"only","name":"one"}`, map[string]string{"Idempotency-Key": "concurrent-key"})
		}()
	}
	wg.Wait()
	close(responses)
	for response := range responses {
		assertStateResponse(t, response, http.StatusCreated)
		if decodeStateItem(t, response).ID != "only" {
			t.Fatalf("concurrent response = %s", response.Body.String())
		}
	}
	listed := stateRequest(t, handler, http.MethodGet, "/state/concurrent/items", "", nil)
	var concurrentItems []Item
	if err := json.Unmarshal(listed.Body.Bytes(), &concurrentItems); err != nil || len(concurrentItems) != 1 {
		t.Fatalf("concurrent list = %s err=%v", listed.Body.String(), err)
	}
}

func TestConcurrentStateNamespaceIsolation(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	const workers = 32
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			namespace := fmt.Sprintf("tenant-%02d", worker)
			for round := 0; round < 8; round++ {
				id := fmt.Sprintf("item-%02d", round)
				created := stateRequest(t, handler, http.MethodPost, "/state/"+namespace+"/items", fmt.Sprintf(`{"id":%q,"name":%q}`, id, namespace), nil)
				if created.Code != http.StatusCreated {
					t.Errorf("create %s/%s = %d %s", namespace, id, created.Code, created.Body.String())
					return
				}
				updated := stateRequest(t, handler, http.MethodPut, "/state/"+namespace+"/items/"+id, `{"name":"updated"}`, map[string]string{"If-Match": `"v1"`})
				if updated.Code != http.StatusOK {
					t.Errorf("update %s/%s = %d %s", namespace, id, updated.Code, updated.Body.String())
					return
				}
			}
		}()
	}
	wg.Wait()
	for worker := 0; worker < workers; worker++ {
		namespace := fmt.Sprintf("tenant-%02d", worker)
		response := stateRequest(t, handler, http.MethodGet, "/state/"+namespace+"/items", "", nil)
		var items []Item
		if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil || len(items) != 8 {
			t.Fatalf("list %s = %s err=%v", namespace, response.Body.String(), err)
		}
		for _, item := range items {
			if item.Namespace != namespace || item.Name != "updated" || item.Version != 2 {
				t.Fatalf("namespace %s contains %+v", namespace, item)
			}
		}
	}
}

func TestConcurrentStateSameNamespaceHasValidVersions(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	assertStateResponse(t, stateRequest(t, handler, http.MethodPost, "/state/shared/items", `{"id":"item","name":"initial"}`, nil), http.StatusCreated)

	const workers = 32
	var wg sync.WaitGroup
	type concurrentResponse struct {
		update   bool
		response *httptest.ResponseRecorder
	}
	responses := make(chan concurrentResponse, workers*2)
	for worker := 0; worker < workers; worker++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			responses <- concurrentResponse{response: stateRequest(t, handler, http.MethodGet, "/state/shared/items/item", "", nil)}
		}()
		go func(worker int) {
			defer wg.Done()
			responses <- concurrentResponse{update: true, response: stateRequest(t, handler, http.MethodPut, "/state/shared/items/item", fmt.Sprintf(`{"name":"worker-%d"}`, worker), map[string]string{"If-Match": `"v1"`})}
		}(worker)
	}
	wg.Wait()
	close(responses)
	updates := 0
	for result := range responses {
		response := result.response
		switch response.Code {
		case http.StatusOK:
			assertStateResponse(t, response, http.StatusOK)
			item := decodeStateItem(t, response)
			if item.Version != 1 && item.Version != 2 {
				t.Fatalf("successful concurrent version = %d", item.Version)
			}
			if result.update && item.Version == 2 {
				updates++
			}
		case http.StatusPreconditionFailed:
			assertStateError(t, response, http.StatusPreconditionFailed)
		default:
			t.Fatalf("concurrent response = %d %s", response.Code, response.Body.String())
		}
	}
	if updates != 1 {
		t.Fatalf("successful updates = %d, want 1", updates)
	}
	final := stateRequest(t, handler, http.MethodGet, "/state/shared/items/item", "", nil)
	if final.Header().Get("ETag") != `"v2"` || decodeStateItem(t, final).Version != 2 {
		t.Fatalf("final state = ETag %q body %s", final.Header().Get("ETag"), final.Body.String())
	}
}

func TestStateResetRejectsLateMutationAndClearsIdempotency(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := newStateRequest(http.MethodPost, "/state/reset/items", `{"id":"late","name":"old"}`, map[string]string{"Idempotency-Key": "reset-key"})
		done <- serveRequest(handler, withStateMutationPause(req, started, release))
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("mutation did not reach pause")
	}
	if response := stateRequest(t, handler, http.MethodPost, "/__test/reset", "", nil); response.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %s", response.Code, response.Body.String())
	}
	close(release)
	select {
	case response := <-done:
		assertStateError(t, response, http.StatusConflict)
	case <-time.After(time.Second):
		t.Fatal("late mutation did not finish")
	}
	listed := stateRequest(t, handler, http.MethodGet, "/state/reset/items", "", nil)
	if listed.Code != http.StatusOK || listed.Body.String() != "[]\n" {
		t.Fatalf("list after reset = %d %s", listed.Code, listed.Body.String())
	}
	created := stateRequest(t, handler, http.MethodPost, "/state/reset/items", `{"id":"new","name":"fresh"}`, map[string]string{"Idempotency-Key": "reset-key"})
	assertStateResponse(t, created, http.StatusCreated)
}

func TestStateResetAlsoResetsFaultSequence(t *testing.T) {
	handler, cleanup := New(Config{Secret: testSecret})
	t.Cleanup(cleanup)
	sequence := func() []int {
		statuses := make([]int, 8)
		for index := range statuses {
			statuses[index] = stateRequest(t, handler, http.MethodGet, "/fault/random?rate=0.5", "", nil).Code
		}
		return statuses
	}
	first := sequence()
	if response := stateRequest(t, handler, http.MethodPost, "/__test/reset", "", nil); response.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %s", response.Code, response.Body.String())
	}
	second := sequence()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("fault sequences = %v and %v", first, second)
	}
}

func stateRequest(t *testing.T, handler http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	return serveRequest(handler, newStateRequest(method, target, body, headers))
}

func newStateRequest(method, target, body string, headers map[string]string) *http.Request {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	request.Header.Set("X-Request-ID", "state-request-id")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return request
}

func assertStateResponse(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status || response.Header().Get("Content-Type") != "application/json" || response.Header().Get("X-Request-ID") == "" {
		t.Fatalf("response = %d content-type=%q request-id=%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Header().Get("X-Request-ID"), response.Body.String())
	}
	var value any
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatalf("invalid JSON: %v body=%s", err, response.Body.String())
	}
	assertNoInternalLeak(t, response.Body.String())
}

func assertStateError(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	assertJSONError(t, response.Code, response.Header().Get("Content-Type"), response.Header().Get("X-Request-ID"), response.Body.Bytes(), status, http.StatusText(status))
	assertNoInternalLeak(t, response.Body.String())
}

func decodeStateItem(t *testing.T, response *httptest.ResponseRecorder) Item {
	t.Helper()
	var item Item
	if err := json.Unmarshal(response.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	return item
}
