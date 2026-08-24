package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/sofiworker/gk/ghttp"
)

type idempotencyRecord struct {
	payload  string
	item     Item
	location string
}

type stateController struct {
	mu         sync.Mutex
	store      *Store
	generation uint64
	records    map[string]idempotencyRecord
}

func newStateController(store *Store) *stateController {
	return &stateController{store: store, records: make(map[string]idempotencyRecord)}
}

func (c *stateController) snapshotGeneration() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation
}

func (c *stateController) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	c.records = make(map[string]idempotencyRecord)
	c.store.Reset()
}

func (c *stateController) create(generation uint64, namespace, key, id, name string) (Item, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		return Item{}, "", ErrVersionConflict
	}
	payload := id + "\x00" + name
	recordKey := namespace + "\x00" + key
	if key != "" {
		if record, ok := c.records[recordKey]; ok {
			if record.payload != payload {
				return Item{}, "", ErrItemExists
			}
			return record.item, record.location, nil
		}
	}
	item, err := c.store.Create(namespace, id, name)
	if err != nil {
		return Item{}, "", err
	}
	location := stateItemPath(namespace, id)
	if key != "" {
		c.records[recordKey] = idempotencyRecord{payload: payload, item: item, location: location}
	}
	return item, location, nil
}

func (c *stateController) update(generation uint64, namespace, id, name string, version uint64) (Item, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		return Item{}, ErrVersionConflict
	}
	return c.store.Update(namespace, id, name, version)
}

func (c *stateController) delete(generation uint64, namespace, id string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		return false, ErrVersionConflict
	}
	return c.store.Delete(namespace, id), nil
}

// stateHandlerFunc 接收路径参数 namespace/id 的原始处理器;collection 路由 id 为空。
// stateHandlerFunc is a raw handler receiving path params namespace/id; id is
// empty for the collection route.
type stateHandlerFunc func(w http.ResponseWriter, r *http.Request, namespace, id string) error

// stateEndpoint 把 stateHandlerFunc 接到 ghttp 路由,从 req.Params 提取 namespace/id。
// stateEndpoint wires a stateHandlerFunc to a ghttp route, extracting
// namespace/id from req.Params.
func stateEndpoint(h stateHandlerFunc) ghttp.RawHandlerFunc {
	return func(_ context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		return h(resp, req.Request, req.Params.Get("namespace"), req.Params.Get("id"))
	}
}

func registerState(server *ghttp.Server, controller *stateController) {
	mustRaw(server.RawHandle(http.MethodPost, "/state/{namespace}/items", stateEndpoint(func(w http.ResponseWriter, r *http.Request, namespace, _ string) error {
		generation := controller.snapshotGeneration()
		pauseStateMutation(r)
		var input struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if !decodeStateJSON(r, &input) || input.ID == "" || input.Name == "" {
			writePublicError(w, http.StatusBadRequest)
			return nil
		}
		item, location, err := controller.create(generation, namespace, r.Header.Get("Idempotency-Key"), input.ID, input.Name)
		if err != nil {
			writeStateMutationError(w, err)
			return nil
		}
		w.Header().Set("Location", location)
		w.Header().Set("ETag", stateETag(item.Version))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		writeJSONBody(w, item)
		return nil
	})))

	mustRaw(server.RawHandle(http.MethodGet, "/state/{namespace}/items/{id}", stateEndpoint(func(w http.ResponseWriter, _ *http.Request, namespace, id string) error {
		item, ok := controller.store.Get(namespace, id)
		if !ok {
			writePublicError(w, http.StatusNotFound)
			return nil
		}
		w.Header().Set("ETag", stateETag(item.Version))
		writeJSON(w, item)
		return nil
	})))

	mustRaw(server.RawHandle(http.MethodGet, "/state/{namespace}/items", stateEndpoint(func(w http.ResponseWriter, _ *http.Request, namespace, _ string) error {
		writeJSON(w, controller.store.List(namespace))
		return nil
	})))

	mustRaw(server.RawHandle(http.MethodPut, "/state/{namespace}/items/{id}", stateEndpoint(func(w http.ResponseWriter, r *http.Request, namespace, id string) error {
		generation := controller.snapshotGeneration()
		pauseStateMutation(r)
		ifMatch := r.Header.Get("If-Match")
		if ifMatch == "" {
			writePublicError(w, http.StatusPreconditionRequired)
			return nil
		}
		version, ok := parseStateETag(ifMatch)
		if !ok {
			writePublicError(w, http.StatusPreconditionFailed)
			return nil
		}
		var input struct {
			Name string `json:"name"`
		}
		if !decodeStateJSON(r, &input) || input.Name == "" {
			writePublicError(w, http.StatusBadRequest)
			return nil
		}
		item, err := controller.update(generation, namespace, id, input.Name, version)
		if err != nil {
			if errors.Is(err, errItemNotFound) {
				writePublicError(w, http.StatusNotFound)
			} else {
				writePublicError(w, http.StatusPreconditionFailed)
			}
			return nil
		}
		w.Header().Set("ETag", stateETag(item.Version))
		writeJSON(w, item)
		return nil
	})))

	mustRaw(server.RawHandle(http.MethodDelete, "/state/{namespace}/items/{id}", stateEndpoint(func(w http.ResponseWriter, r *http.Request, namespace, id string) error {
		generation := controller.snapshotGeneration()
		pauseStateMutation(r)
		deleted, err := controller.delete(generation, namespace, id)
		if err != nil {
			writePublicError(w, http.StatusConflict)
			return nil
		}
		if !deleted {
			writePublicError(w, http.StatusNotFound)
			return nil
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})))
}

func decodeStateJSON(request *http.Request, target any) bool {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

func writeStateMutationError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrItemExists) || errors.Is(err, ErrVersionConflict) {
		writePublicError(w, http.StatusConflict)
		return
	}
	writePublicError(w, http.StatusInternalServerError)
}

func stateETag(version uint64) string { return fmt.Sprintf(`"v%d"`, version) }

func parseStateETag(value string) (uint64, bool) {
	if strings.Contains(value, ",") || strings.HasPrefix(value, "W/") || len(value) < 4 || value[0] != '"' || value[len(value)-1] != '"' {
		return 0, false
	}
	inner := value[1 : len(value)-1]
	if !strings.HasPrefix(inner, "v") {
		return 0, false
	}
	version, err := strconv.ParseUint(strings.TrimPrefix(inner, "v"), 10, 64)
	return version, err == nil && version > 0
}

func stateItemPath(namespace, id string) string {
	return "/state/" + namespace + "/items/" + id
}

func writeJSONBody(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

type stateMutationPause struct {
	started chan struct{}
	release chan struct{}
}

type stateMutationPauseContextKey struct{}

func withStateMutationPause(request *http.Request, started, release chan struct{}) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), stateMutationPauseContextKey{}, stateMutationPause{started: started, release: release}))
}

func pauseStateMutation(request *http.Request) {
	pause, ok := request.Context().Value(stateMutationPauseContextKey{}).(stateMutationPause)
	if !ok {
		return
	}
	close(pause.started)
	<-pause.release
}
