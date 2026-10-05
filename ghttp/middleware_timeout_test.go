package ghttp

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestTimeout(t *testing.T) {
	custom := errors.New("custom timeout")
	waitCtx := func(ctx context.Context, _ *Request, _ *Response) error {
		<-ctx.Done()
		return nil
	}
	tests := []struct {
		name    string
		d       time.Duration
		opts    []TimeoutOption
		h       Handler
		wantErr func(error) bool
	}{
		{"fast", time.Second, nil, func(context.Context, *Request, *Response) error { return nil },
			func(err error) bool { return err == nil }},
		{"slow ctx-aware returns nil", 10 * time.Millisecond, nil, waitCtx, func(err error) bool {
			return errors.Is(err, ErrServiceUnavailable) && errors.Is(err, context.DeadlineExceeded)
		}},
		{"deadline error", 10 * time.Millisecond, nil, func(ctx context.Context, _ *Request, _ *Response) error {
			<-ctx.Done()
			return ctx.Err()
		}, func(err error) bool { return errors.Is(err, ErrServiceUnavailable) }},
		{"custom error", 10 * time.Millisecond, []TimeoutOption{WithTimeoutError(custom), WithTimeoutError(nil)}, waitCtx,
			func(err error) bool { return err == custom }},
		{"written then timeout", 10 * time.Millisecond, nil, func(ctx context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(200)
			<-ctx.Done()
			return nil
		}, func(err error) bool { return err == nil }},
		{"other error preserved", time.Second, nil, func(context.Context, *Request, *Response) error { return ErrNotFound },
			func(err error) bool { return errors.Is(err, ErrNotFound) }},
		{"zero passthrough", 0, nil, func(ctx context.Context, _ *Request, _ *Response) error {
			if _, ok := ctx.Deadline(); ok {
				return errors.New("unexpected deadline")
			}
			return nil
		}, func(err error) bool { return err == nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &Request{Raw: newTestReq()}
			err := Timeout(tt.d, tt.opts...)(tt.h)(context.Background(), req, newTestResp())
			if !tt.wantErr(err) {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestTimeoutServer503(t *testing.T) {
	w := ridServe(t, Timeout(10*time.Millisecond), func(ctx context.Context, req *Request, _ *Response) error {
		<-req.Context().Done()
		return nil
	}, nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code %d", w.Code)
	}
}
