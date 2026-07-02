package ghttp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// SSEWriter writes Server-Sent Events to a response.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// SSEHandler handles an SSE connection.
type SSEHandler func(ctx context.Context, params Params, stream *SSEWriter) error

// SSEEvent represents a single SSE event.
type SSEEvent struct {
	ID    string
	Event string
	Data  string
}

func (s *SSEWriter) WriteEvent(event, data string) error {
	_, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data)
	if err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

func (s *SSEWriter) WriteJSON(event string, data interface{}) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return s.WriteEvent(event, string(b))
}

// SSEStream is a client-side SSE stream.
type SSEStream struct {
	Events  <-chan SSEEvent
	Errors  <-chan error
	closeFn context.CancelFunc
}

// SSEConfig configures client SSE behavior.
type SSEConfig struct {
	OnEvent   func(SSEEvent)
	OnError   func(error)
	OnConnect func(*http.Response)
}

func (c *Client) SSE(url string, config ...SSEConfig) (*SSEStream, error) {
	cfg := SSEConfig{}
	if len(config) > 0 {
		cfg = config[0]
	}

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}

	if cfg.OnConnect != nil {
		cfg.OnConnect(resp)
	}

	events := make(chan SSEEvent, 64)
	errs := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer resp.Body.Close()
		defer close(events)
		defer close(errs)

		scanner := bufio.NewScanner(resp.Body)
		var event SSEEvent

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "id: "):
				event.ID = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				event.Event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				event.Data = strings.TrimPrefix(line, "data: ")
			case line == "":
				if cfg.OnEvent != nil {
					cfg.OnEvent(event)
				}
				select {
				case events <- event:
				default:
				}
				event = SSEEvent{}
			}
		}

		if err := scanner.Err(); err != nil && err != io.EOF {
			if cfg.OnError != nil {
				cfg.OnError(err)
			}
			errs <- err
		}
	}()

	return &SSEStream{
		Events:  events,
		Errors:  errs,
		closeFn: cancel,
	}, nil
}

func (s *SSEStream) Close() {
	if s.closeFn != nil {
		s.closeFn()
	}
}
