package gclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type SSEEvent struct {
	ID    string
	Event string
	Data  string
	Retry int
}

type SSEHandler func(event SSEEvent) error

type SSERequest struct {
	*Request
	handler     SSEHandler
	onConnect   func(*http.Response) error
	onRetry     func(int, error, time.Duration)
	onError     func(error)
	observer    StreamObserver
	reconnect   bool
	retryDelay  time.Duration
	maxRetries  int
	lastEventID string
}

type SSEStream struct {
	Events <-chan SSEEvent
	Errors <-chan error
}

type SSEJSONStream struct {
	Events <-chan interface{}
	Errors <-chan error
}

func (e SSEEvent) DecodeJSON(target interface{}) error {
	if target == nil {
		return nil
	}
	return json.Unmarshal([]byte(e.Data), target)
}

func NewSSERequest(r *Request) *SSERequest {
	if r == nil {
		r = NewClient().R()
	}
	return &SSERequest{
		Request:    r,
		reconnect:  true,
		retryDelay: 3 * time.Second,
		maxRetries: 5,
	}
}

func (c *Client) NewSSERequest() *SSERequest {
	return NewSSERequest(c.R())
}

func (r *Request) NewSSERequest() *SSERequest {
	return NewSSERequest(r)
}

func (r *Request) SetSSEHandler(handler SSEHandler) *SSERequest {
	return NewSSERequest(r).SetHandler(handler)
}

func (s *SSERequest) SetHandler(handler SSEHandler) *SSERequest {
	s.handler = handler
	return s
}

func (s *SSERequest) OnConnect(fn func(*http.Response) error) *SSERequest {
	s.onConnect = fn
	return s
}

func (s *SSERequest) OnRetry(fn func(int, error, time.Duration)) *SSERequest {
	s.onRetry = fn
	return s
}

func (s *SSERequest) OnError(fn func(error)) *SSERequest {
	s.onError = fn
	return s
}

func (s *SSERequest) SetObserver(observer StreamObserver) *SSERequest {
	s.observer = observer
	return s
}

func (s *SSERequest) SetReconnect(reconnect bool) *SSERequest {
	s.reconnect = reconnect
	return s
}

func (s *SSERequest) SetRetryDelay(delay time.Duration) *SSERequest {
	s.retryDelay = delay
	return s
}

func (s *SSERequest) SetMaxRetries(maxRetries int) *SSERequest {
	s.maxRetries = maxRetries
	return s
}

func (s *SSERequest) SetLastEventID(id string) *SSERequest {
	s.lastEventID = id
	return s
}

func (s *SSERequest) Stream(ctx context.Context) error {
	if s.handler == nil {
		return errors.New("sse handler is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	retries := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.prepareRequest(ctx)
		httpResp, err := req.effectiveClient().stream(req)
		if err != nil {
			s.callOnError(err)
			if !s.shouldReconnect(retries, err) {
				return err
			}
			s.callOnRetry(retries+1, err)
			retries++
			if err := sleepWithContext(ctx, s.retryDelay); err != nil {
				return err
			}
			continue
		}
		if httpResp.StatusCode != http.StatusOK {
			_ = httpResp.Body.Close()
			err = fmt.Errorf("unexpected status code %d", httpResp.StatusCode)
			s.callOnError(err)
			if !s.shouldReconnect(retries, err) {
				return err
			}
			s.callOnRetry(retries+1, err)
			retries++
			if err := sleepWithContext(ctx, s.retryDelay); err != nil {
				return err
			}
			continue
		}
		if s.onConnect != nil {
			if err := s.onConnect(httpResp); err != nil {
				_ = httpResp.Body.Close()
				s.callOnError(err)
				return err
			}
		}
		if s.observer != nil {
			if err := s.observer.OnConnect(StreamConnectInfo{
				Protocol: StreamProtocolSSE,
				URL:      req.URL,
				Response: httpResp,
			}); err != nil {
				_ = httpResp.Body.Close()
				s.callOnError(err)
				return err
			}
		}

		err = s.handleSSEStream(ctx, httpResp.Body)
		_ = httpResp.Body.Close()
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			s.callOnClose(err, req.URL)
			return err
		}
		s.callOnError(err)
		if !s.shouldReconnect(retries, err) {
			s.callOnClose(err, req.URL)
			return err
		}
		s.callOnRetry(retries+1, err)
		retries++
		if err := sleepWithContext(ctx, s.retryDelay); err != nil {
			return err
		}
	}
}

func (s *SSERequest) StreamChannels(ctx context.Context, buffer int) *SSEStream {
	events := make(chan SSEEvent, buffer)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		err := s.cloneWithHandler(func(event SSEEvent) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case events <- event:
				return nil
			}
		}).Stream(ctx)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			errs <- err
		}
	}()

	return &SSEStream{
		Events: events,
		Errors: errs,
	}
}

func (s *SSERequest) StreamJSONChannels(ctx context.Context, eventName string, buffer int, factory func() interface{}) *SSEJSONStream {
	events := make(chan interface{}, buffer)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		if factory == nil {
			errs <- errors.New("json event factory is required")
			return
		}

		err := s.cloneWithHandler(func(event SSEEvent) error {
			if eventName != "" && event.Event != eventName {
				return nil
			}
			target := factory()
			if err := event.DecodeJSON(target); err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case events <- target:
				return nil
			}
		}).Stream(ctx)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			errs <- err
		}
	}()

	return &SSEJSONStream{
		Events: events,
		Errors: errs,
	}
}

func (s *SSERequest) cloneWithHandler(handler SSEHandler) *SSERequest {
	if s == nil {
		return nil
	}
	cp := *s
	cp.Request = s.Request.Clone()
	cp.handler = handler
	return &cp
}

func (s *SSERequest) callOnRetry(attempt int, err error) {
	if s.onRetry != nil {
		s.onRetry(attempt, err, s.retryDelay)
	}
	if s.observer != nil {
		s.observer.OnRetry(StreamRetryInfo{
			Protocol: StreamProtocolSSE,
			URL:      s.Request.URL,
			Attempt:  attempt,
			Delay:    s.retryDelay,
			Err:      err,
		})
	}
}

func (s *SSERequest) callOnError(err error) {
	if err != nil && s.onError != nil {
		s.onError(err)
	}
	if err != nil && s.observer != nil {
		s.observer.OnError(StreamProtocolSSE, s.Request.URL, err)
	}
}

func (s *SSERequest) callOnClose(err error, rawURL string) {
	if s.observer != nil {
		s.observer.OnClose(StreamCloseInfo{
			Protocol: StreamProtocolSSE,
			URL:      rawURL,
			Err:      err,
		})
	}
}

func (s *SSERequest) Filter(eventName string) *SSERequest {
	return s.cloneWithHandler(func(event SSEEvent) error {
		if eventName == "" || event.Event == eventName {
			if s.handler != nil {
				return s.handler(event)
			}
		}
		return nil
	})
}

func (s *SSERequest) OnJSON(eventName string, target interface{}, handler func(interface{}) error) *SSERequest {
	return s.cloneWithHandler(func(event SSEEvent) error {
		if eventName != "" && event.Event != eventName {
			return nil
		}
		if err := event.DecodeJSON(target); err != nil {
			return err
		}
		if handler != nil {
			return handler(target)
		}
		return nil
	})
}

func (s *SSERequest) prepareRequest(ctx context.Context) *Request {
	req := s.Request.Clone()
	req.SetMethod(http.MethodGet)
	req.SetHeader("Accept", "text/event-stream")
	req.SetHeader("Cache-Control", "no-cache")
	req.SetContext(ctx)
	if s.lastEventID != "" {
		req.SetHeader("Last-Event-ID", s.lastEventID)
	}
	return req
}

func (s *SSERequest) shouldReconnect(retries int, _ error) bool {
	return s.reconnect && (s.maxRetries <= 0 || retries < s.maxRetries)
}

func (s *SSERequest) handleSSEStream(ctx context.Context, stream io.Reader) error {
	scanner := bufio.NewScanner(stream)
	buf := make([]byte, 0, 128*1024)
	scanner.Buffer(buf, 512*1024)

	var currentEvent SSEEvent
	flush := func() error {
		if currentEvent.ID == "" && currentEvent.Event == "" && currentEvent.Data == "" && currentEvent.Retry == 0 {
			return nil
		}
		if err := s.handler(currentEvent); err != nil {
			return err
		}
		currentEvent = SSEEvent{}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()

		select {
		case <-ctx.Done():
			if err := flush(); err != nil {
				return err
			}
			return ctx.Err()
		default:
		}

		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		field := strings.TrimSpace(parts[0])
		value := ""
		if len(parts) == 2 {
			value = strings.TrimSpace(parts[1])
		}

		switch field {
		case "id":
			currentEvent.ID = value
			s.lastEventID = value
		case "event":
			currentEvent.Event = value
		case "data":
			if currentEvent.Data != "" {
				currentEvent.Data += "\n"
			}
			currentEvent.Data += value
		case "retry":
			if _, err := fmt.Sscanf(value, "%d", &currentEvent.Retry); err == nil && currentEvent.Retry > 0 {
				s.retryDelay = time.Duration(currentEvent.Retry) * time.Millisecond
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
