package app

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/sofiworker/gk/ghttp"
)

// FaultController 为测试故障路由提供可重置的确定性序列与并发门控。
// FaultController provides resettable deterministic sequencing and concurrency gating for test fault routes.
type FaultController struct {
	sequence atomic.Uint64
	gate     chan struct{}
}

// NewFaultController 创建容量为一的故障控制器。
// NewFaultController creates a fault controller with a capacity-one gate.
func NewFaultController() *FaultController {
	return &FaultController{gate: make(chan struct{}, 1)}
}

// Reset 重置确定性随机序列。
// Reset resets the deterministic random sequence.
func (c *FaultController) Reset() { c.sequence.Store(0) }

const (
	maxFaultDelay  = 100 * time.Millisecond
	faultTimeout   = 20 * time.Millisecond
	faultLateWrite = 200 * time.Millisecond
)

func registerFaults(server *ghttp.Server, cfg Config, controller *FaultController) {
	secret := cfg.Secret
	if secret == "" {
		secret = testDefaultSecret
	}
	faults := server.Group("/fault")
	faults.MustMount(ghttp.RawOperation(http.MethodGet, "/delay", http.HandlerFunc(handleFaultDelay)))
	faults.MustMount(ghttp.RawOperation(http.MethodGet, "/random", http.HandlerFunc(controller.handleRandom)))
	faults.MustMount(ghttp.RawOperation(http.MethodGet, "/limited", http.HandlerFunc(controller.handleLimited)))
	faults.MustMount(ghttp.RawOperation(http.MethodPost, "/reset", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		controller.Reset()
		w.WriteHeader(http.StatusNoContent)
	})))
	faults.MustMount(ghttp.RawOperation(http.MethodGet, "/unavailable", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writePublicError(w, http.StatusServiceUnavailable)
	})))
	faults.MustMount(ghttp.RawOperation(http.MethodGet, "/panic", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("fault panic " + secret)
	})))
	faults.MustMount(ghttp.RawOperation(http.MethodGet, "/timeout", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(faultLateWrite)
		writeJSON(w, map[string]string{"status": "late-success"})
		signalFaultLateDone(r)
	})).WithMiddleware(ghttp.Timeout(faultTimeout)))

	faults.MustMount(ghttp.RawOperation(http.MethodGet, "/cancel", http.HandlerFunc(handleFaultCancel)))
}

func handleFaultDelay(w http.ResponseWriter, r *http.Request) {
	delay, err := boundedDelay(r.URL.Query().Get("ms"))
	if err != nil {
		writePublicError(w, http.StatusBadRequest)
		return
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		writeJSON(w, map[string]int64{"delay_ms": delay.Milliseconds()})
	case <-r.Context().Done():
		w.Header().Set("X-Cancel-Observed", "true")
		writePublicError(w, http.StatusRequestTimeout)
	}
}

func boundedDelay(raw string) (time.Duration, error) {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid delay")
	}
	delay := time.Duration(value) * time.Millisecond
	if delay > maxFaultDelay {
		delay = maxFaultDelay
	}
	return delay, nil
}

func (c *FaultController) handleRandom(w http.ResponseWriter, r *http.Request) {
	rate, err := strconv.ParseFloat(r.URL.Query().Get("rate"), 64)
	if err != nil || rate < 0 || rate > 1 {
		writePublicError(w, http.StatusBadRequest)
		return
	}
	switch r.Header.Get("X-Fault-Result") {
	case "fail":
		writePublicError(w, http.StatusServiceUnavailable)
	case "pass":
		writeJSON(w, map[string]any{"failed": false, "rate": rate})
	default:
		sequence := c.sequence.Add(1) - 1
		bucket := (sequence * 37) % 100
		if float64(bucket) < rate*100 {
			writePublicError(w, http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, map[string]any{"failed": false, "rate": rate})
	}
}

func (c *FaultController) handleLimited(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Fault-Result") == "reject" {
		writePublicError(w, http.StatusTooManyRequests)
		return
	}
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	default:
		writePublicError(w, http.StatusTooManyRequests)
		return
	}
	if r.URL.Query().Get("hold") == "1" {
		signalFaultStarted(r)
		<-r.Context().Done()
		writePublicError(w, http.StatusRequestTimeout)
		return
	}
	writeJSON(w, map[string]string{"status": "admitted"})
}

func handleFaultCancel(w http.ResponseWriter, r *http.Request) {
	wait, err := boundedWait(r.URL.Query().Get("wait_ms"))
	if err != nil {
		writePublicError(w, http.StatusBadRequest)
		return
	}
	signalFaultStarted(r)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-r.Context().Done():
		w.Header().Set("X-Cancel-Observed", "true")
		writePublicError(w, http.StatusRequestTimeout)
	case <-timer.C:
		writeJSON(w, map[string]string{"status": "success"})
	}
}

func boundedWait(raw string) (time.Duration, error) {
	if raw == "" {
		return maxFaultDelay, nil
	}
	return boundedDelay(raw)
}

type faultStartedContextKey struct{}
type faultLateDoneContextKey struct{}

func withFaultStarted(request *http.Request, started chan struct{}) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), faultStartedContextKey{}, started))
}
func withFaultLateDone(request *http.Request, done chan struct{}) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), faultLateDoneContextKey{}, done))
}
func signalFaultStarted(request *http.Request) {
	if started, ok := request.Context().Value(faultStartedContextKey{}).(chan struct{}); ok {
		started <- struct{}{}
	}
}
func signalFaultLateDone(request *http.Request) {
	if done, ok := request.Context().Value(faultLateDoneContextKey{}).(chan struct{}); ok {
		close(done)
	}
}
