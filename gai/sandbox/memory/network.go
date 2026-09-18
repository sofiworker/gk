package memory

import (
	"context"
	"net/url"
	"time"

	s "github.com/sofiworker/gk/gai/sandbox"
)

// Request 仅记录方法和不含查询参数的目标，不采集请求或响应正文、头及凭据。
// Request records only method and query-free destination, never bodies, headers or credentials.
func (b *Sandbox) Request(ctx context.Context, c s.Call, in s.HTTPRequest) (s.HTTPResponse, error) {
	if err := b.lock(ctx); err != nil {
		return s.HTTPResponse{}, err
	}
	if len(in.URL) > 8192 || len(c.ID)+len(c.SessionID)+len(c.TurnID)+len(c.ToolCallID) > 8192 {
		b.unlock()
		return s.HTTPResponse{}, s.ErrQuota
	}
	headerBytes := 0
	for k, vs := range in.Header {
		headerBytes += len(k)
		for _, v := range vs {
			headerBytes += len(v)
		}
	}
	if int64(len(in.Body)) > b.cfg.limits.FileBytes || headerBytes > 16384 {
		b.unlock()
		return s.HTTPResponse{}, s.ErrQuota
	}
	if err := b.eventSpace(2); err != nil {
		b.unlock()
		return s.HTTPResponse{}, err
	}
	if b.cfg.network == nil {
		b.addEvent(s.Event{OperationID: c.ID, Kind: "http_denied", Error: s.ErrDenied.Error(), After: b.revision})
		b.unlock()
		return s.HTTPResponse{}, s.ErrDenied
	}
	u, err := url.Parse(in.URL)
	if err != nil {
		b.unlock()
		return s.HTTPResponse{}, s.ErrPath
	}
	target := u.Scheme + "://" + u.Host
	if c.ID == "" {
		c.ID = b.id("http")
	}
	e := s.Event{OperationID: c.ID, Attribution: attribution(c), Kind: "http_start", Path: target, Destination: in.Method, After: b.revision, StartedAt: time.Now().UnixMilli(), RequestBytes: int64(len(in.Body))}
	if b.usage(b.tree, nil)+int64(len(target)+len(c.ID)+len(in.Body)+2048) > b.cfg.limits.Bytes {
		b.unlock()
		return s.HTTPResponse{}, s.ErrQuota
	}
	reserved := int64(len(in.Body) + headerBytes + 32768)
	if b.usage(b.tree, nil)+reserved+32768 > b.cfg.limits.Bytes {
		b.unlock()
		return s.HTTPResponse{}, s.ErrQuota
	}
	b.inflightBytes += reserved
	b.addEvent(e)
	child, token := b.beginExternal(ctx)
	sender := b.cfg.network
	in.Header = in.Header.Clone()
	in.Body = append([]byte(nil), in.Body...)
	b.unlock()
	out, sendErr := safeSend(child, sender, in)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.endExternal(token)
	b.inflightBytes -= reserved
	if int64(len(out.Body)) > b.cfg.limits.FileBytes {
		out = s.HTTPResponse{}
		sendErr = s.ErrQuota
	}
	e.Kind = "http_end"
	e.StatusCode = out.StatusCode
	e.ResponseBytes = int64(len(out.Body))
	e.After = b.revision
	// 不记录自定义传输错误原文，避免错误文本包含令牌或完整 URL。
	// Do not record custom transport error text, which may include tokens or full URLs.
	if sendErr != nil {
		e.Error = s.ErrUnknown.Error()
	}
	b.addEvent(e)
	return out, sendErr
}
func safeSend(ctx context.Context, sender s.HTTPSender, in s.HTTPRequest) (out s.HTTPResponse, err error) {
	defer func() {
		if recover() != nil {
			err = s.ErrUnknown
		}
	}()
	return sender.Send(ctx, in)
}

// SetNetwork 替换可信网络能力，nil 撤销；不允许与进行中的外部操作并发。
// SetNetwork replaces the trusted network capability; nil revokes it, and active external operations prevent replacement.
func (b *Sandbox) SetNetwork(ctx context.Context, sender s.HTTPSender) error {
	if err := b.lock(ctx); err != nil {
		return err
	}
	defer b.unlock()
	if b.active > 0 {
		return s.ErrBusy
	}
	if err := b.eventSpace(1); err != nil {
		return err
	}
	b.cfg.network = sender
	b.policy++
	b.addEvent(s.Event{Kind: "network_policy", After: b.revision})
	return nil
}
