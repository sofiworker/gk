package rawprobe

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Config controls post-case health probes.
// Config 用于控制 case 后置健康探针。
type Config struct {
	HealthURL  string
	MetricsURL string
}

func Run(ctx context.Context, addr string, cases []Case) Report {
	return RunWithConfig(ctx, addr, cases, Config{})
}
func RunWithConfig(ctx context.Context, addr string, cases []Case, cfg Config) Report {
	var r Report
	for _, c := range cases {
		if ctx.Err() != nil {
			break
		}
		res := runCase(ctx, addr, c)
		if cfg.HealthURL != "" {
			if err := probeURL(ctx, cfg.HealthURL); err != nil {
				res.Healthy = false
				res.Class = "health_failure"
				res.Detail = err.Error()
			}
		}
		if cfg.MetricsURL != "" {
			if err := probeMetrics(ctx, cfg.MetricsURL); err != nil {
				res.Healthy = false
				res.Class = "recovery_failure"
				res.Detail = err.Error()
			}
		}
		r.Results = append(r.Results, res)
		if res.Healthy {
			r.Passed++
		} else {
			r.Failed++
		}
	}
	return r
}

func probeURL(ctx context.Context, addr string) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", addr, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("probe status %d", resp.StatusCode)
	}
	return nil
}
func probeMetrics(ctx context.Context, addr string) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", addr, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return err
	}
	for _, k := range []string{"active_requests", "sse_connections", "ws_connections"} {
		v, ok := m[k]
		n, nok := v.(float64)
		if !ok || !nok {
			return fmt.Errorf("metric %s missing", k)
		}
		// metrics 请求本身计入 active_requests；The metrics request itself counts as active.
		limit := float64(0)
		if k == "active_requests" {
			limit = 1
		}
		if n > limit {
			return fmt.Errorf("metric %s=%v", k, v)
		}
	}
	return nil
}

func runCase(ctx context.Context, addr string, c Case) Result {
	res := Result{Name: c.Name, Class: "invalid_response"}
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		res.Class = "connection"
		res.Detail = err.Error()
		return res
	}
	defer conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	_ = conn.SetDeadline(deadline)
	if len(c.Segments) > 0 {
		for i, seg := range c.Segments {
			if _, err = conn.Write(seg); err != nil {
				break
			}
			if i+1 < len(c.Segments) {
				time.Sleep(time.Duration(c.SegmentDelayMS) * time.Millisecond)
			}
		}
	} else if _, err = conn.Write(c.Payload); err != nil {
		res.Class = "connection"
		res.Detail = err.Error()
		return res
	}
	max := c.MaxResponseLen
	if max <= 0 {
		max = 1 << 20
	}
	// Allow headers to parse while enforcing the configured body/response budget below.
	lr := io.LimitReader(conn, int64(max)+8192)
	br := bufio.NewReader(lr)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			res.Class = "timeout"
			res.Healthy = c.AllowTimeout
		} else if errors.Is(err, io.EOF) {
			res.Class = "connection_close"
		} else {
			res.Class = "invalid_response"
		}
		res.Detail = err.Error()
		if res.Class == "connection_close" {
			res.Healthy = c.ExpectClose
		}
		return res
	}
	res.Status = resp.StatusCode
	if resp.ContentLength > int64(max) {
		res.Class = "response_limit"
		res.Detail = "response exceeds limit"
		_ = resp.Body.Close()
		return res
	}
	if resp.ContentLength > int64(max) {
		res.Class = "response_limit"
		res.Detail = "response exceeds limit"
		return res
	}
	body, berr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if berr != nil {
		res.Detail = berr.Error()
		if errors.Is(berr, io.ErrUnexpectedEOF) {
			res.Class = "truncated"
		} else if ne, ok := berr.(net.Error); ok && ne.Timeout() {
			res.Class = "timeout"
		}
		return res
	}
	if len(body) > max {
		res.Class = "response_limit"
		res.Detail = "response exceeds limit"
		return res
	}
	res.Response = body
	res.Class = "valid_http"
	if c.Secret != "" && (strings.Contains(string(body), c.Secret) || strings.Contains(fmt.Sprintf("%v", resp.Header), c.Secret)) {
		res.Class = "secret_leak"
		res.Detail = "configured secret leaked"
		return res
	}
	if res.Status < 100 || res.Status > 599 {
		res.Healthy = false
		res.Detail = "invalid status"
		return res
	}
	allowed := len(c.AllowedStatus) == 0
	for _, s := range c.AllowedStatus {
		if s == res.Status {
			allowed = true
		}
	}
	res.Healthy = allowed
	if c.ExpectClose {
		// ExpectClose requires observing EOF after the declared response body.
		var one [1]byte
		n, e := conn.Read(one[:])
		if n == 0 && errors.Is(e, io.EOF) {
			res.Class = "connection_close"
			res.Healthy = allowed
		} else if ne, ok := e.(net.Error); ok && ne.Timeout() {
			res.Class = "timeout"
			res.Healthy = false
		} else if n > 0 {
			res.Class = "valid_http"
			res.Healthy = false
		}
	}
	return res
}

func Healthy(ctx context.Context, addr string) error {
	c := Case{Name: "health", Payload: []byte("GET /health HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"), AllowedStatus: []int{200}}
	r := Run(ctx, addr, []Case{c})
	if len(r.Results) == 0 {
		return fmt.Errorf("health probe failed: no result")
	}
	if !r.Results[0].Healthy {
		return fmt.Errorf("health probe failed: %s", strings.TrimSpace(r.Results[0].Detail))
	}
	return nil
}
