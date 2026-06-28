package gserver

import "net/http"

const (
	HeaderAuditAction = "X-Audit-Action"
	auditEventKey     = "_gserver_audit_event"
)

type AuditEvent struct {
	Action       string
	UserID       string
	ResourceType string
	ResourceID   string
	Status       string
	Message      string
	Detail       string
	RequestID    string
	TraceID      string
	ClientIP     string
	Method       string
	Path         string
	Payload      interface{}
}

type AuditBuilder struct {
	ctx *Context
}

type ResponseBuilder struct {
	ctx     *Context
	code    int
	headers map[string]string
}

func (c *Context) Resp() *ResponseBuilder {
	return &ResponseBuilder{
		ctx:  c,
		code: http.StatusOK,
	}
}

func (c *Context) Builder() *ResponseBuilder {
	return c.Resp()
}

func (c *Context) Audit(payload interface{}) *AuditBuilder {
	event := c.auditEvent()
	event.Payload = payload
	c.SetValue(auditEventKey, event)
	return &AuditBuilder{ctx: c}
}

func (c *Context) AuditPayload() (interface{}, bool) {
	event, ok := c.AuditEvent()
	if !ok {
		return nil, false
	}
	return event.Payload, true
}

func (c *Context) AuditInfo() (interface{}, bool) {
	event, ok := c.AuditEvent()
	if !ok {
		return nil, false
	}
	return event, true
}

func (c *Context) AuditEvent() (AuditEvent, bool) {
	value, ok := c.GetValue(auditEventKey)
	if !ok {
		return AuditEvent{}, false
	}
	event, ok := value.(AuditEvent)
	return event, ok
}

func (c *Context) Ok(data interface{}) {
	c.Resp().Ok(data)
}

func (c *Context) auditEvent() AuditEvent {
	event, ok := c.AuditEvent()
	if !ok {
		return AuditEvent{}
	}
	return event
}

func (a *AuditBuilder) Action(action string) *AuditBuilder {
	return a.update(func(event *AuditEvent) {
		event.Action = action
	})
}

func (a *AuditBuilder) UserID(userID string) *AuditBuilder {
	return a.update(func(event *AuditEvent) {
		event.UserID = userID
	})
}

func (a *AuditBuilder) Resource(resourceType, resourceID string) *AuditBuilder {
	return a.update(func(event *AuditEvent) {
		event.ResourceType = resourceType
		event.ResourceID = resourceID
	})
}

func (a *AuditBuilder) Status(status string) *AuditBuilder {
	return a.update(func(event *AuditEvent) {
		event.Status = status
	})
}

func (a *AuditBuilder) Message(message string) *AuditBuilder {
	return a.update(func(event *AuditEvent) {
		event.Message = message
	})
}

func (a *AuditBuilder) Detail(detail string) *AuditBuilder {
	return a.update(func(event *AuditEvent) {
		event.Detail = detail
	})
}

func (a *AuditBuilder) RequestID(requestID string) *AuditBuilder {
	return a.update(func(event *AuditEvent) {
		event.RequestID = requestID
	})
}

func (a *AuditBuilder) TraceID(traceID string) *AuditBuilder {
	return a.update(func(event *AuditEvent) {
		event.TraceID = traceID
	})
}

func (a *AuditBuilder) ClientIP(clientIP string) *AuditBuilder {
	return a.update(func(event *AuditEvent) {
		event.ClientIP = clientIP
	})
}

func (a *AuditBuilder) Method(method string) *AuditBuilder {
	return a.update(func(event *AuditEvent) {
		event.Method = method
	})
}

func (a *AuditBuilder) Path(path string) *AuditBuilder {
	return a.update(func(event *AuditEvent) {
		event.Path = path
	})
}

func (a *AuditBuilder) Payload(payload interface{}) *AuditBuilder {
	return a.update(func(event *AuditEvent) {
		event.Payload = payload
	})
}

func (a *AuditBuilder) update(update func(*AuditEvent)) *AuditBuilder {
	if a == nil || a.ctx == nil {
		return a
	}
	event := a.ctx.auditEvent()
	update(&event)
	a.ctx.SetValue(auditEventKey, event)
	return a
}

func (b *ResponseBuilder) Builder() *ResponseBuilder {
	return b
}

func (b *ResponseBuilder) Status(code int) *ResponseBuilder {
	b.code = code
	return b
}

func (b *ResponseBuilder) OkStatus() *ResponseBuilder {
	return b.Status(http.StatusOK)
}

func (b *ResponseBuilder) CreatedStatus() *ResponseBuilder {
	return b.Status(http.StatusCreated)
}

func (b *ResponseBuilder) NoContentStatus() *ResponseBuilder {
	return b.Status(http.StatusNoContent)
}

func (b *ResponseBuilder) Header(key, value string) *ResponseBuilder {
	if b.headers == nil {
		b.headers = make(map[string]string)
	}
	b.headers[key] = value
	return b
}

func (b *ResponseBuilder) Headers(headers map[string]string) *ResponseBuilder {
	for key, value := range headers {
		b.Header(key, value)
	}
	return b
}

func (b *ResponseBuilder) Audit(payload interface{}) *ResponseBuilder {
	if b != nil && b.ctx != nil {
		b.ctx.Audit(payload)
	}
	return b
}

func (b *ResponseBuilder) AuditAction(action string) *ResponseBuilder {
	if b != nil && b.ctx != nil {
		b.ctx.audit().Action(action)
	}
	return b
}

func (b *ResponseBuilder) AuditUserID(userID string) *ResponseBuilder {
	if b != nil && b.ctx != nil {
		b.ctx.audit().UserID(userID)
	}
	return b
}

func (b *ResponseBuilder) AuditResource(resourceType, resourceID string) *ResponseBuilder {
	if b != nil && b.ctx != nil {
		b.ctx.audit().Resource(resourceType, resourceID)
	}
	return b
}

func (b *ResponseBuilder) AuditStatus(status string) *ResponseBuilder {
	if b != nil && b.ctx != nil {
		b.ctx.audit().Status(status)
	}
	return b
}

func (b *ResponseBuilder) AuditMessage(message string) *ResponseBuilder {
	if b != nil && b.ctx != nil {
		b.ctx.audit().Message(message)
	}
	return b
}

func (b *ResponseBuilder) AuditDetail(detail string) *ResponseBuilder {
	if b != nil && b.ctx != nil {
		b.ctx.audit().Detail(detail)
	}
	return b
}

func (b *ResponseBuilder) AuditRequestID(requestID string) *ResponseBuilder {
	if b != nil && b.ctx != nil {
		b.ctx.audit().RequestID(requestID)
	}
	return b
}

func (b *ResponseBuilder) AuditTraceID(traceID string) *ResponseBuilder {
	if b != nil && b.ctx != nil {
		b.ctx.audit().TraceID(traceID)
	}
	return b
}

func (b *ResponseBuilder) AuditClientIP(clientIP string) *ResponseBuilder {
	if b != nil && b.ctx != nil {
		b.ctx.audit().ClientIP(clientIP)
	}
	return b
}

func (b *ResponseBuilder) AuditMethod(method string) *ResponseBuilder {
	if b != nil && b.ctx != nil {
		b.ctx.audit().Method(method)
	}
	return b
}

func (b *ResponseBuilder) AuditPath(path string) *ResponseBuilder {
	if b != nil && b.ctx != nil {
		b.ctx.audit().Path(path)
	}
	return b
}

func (c *Context) audit() *AuditBuilder {
	if _, ok := c.AuditEvent(); !ok {
		c.SetValue(auditEventKey, AuditEvent{})
	}
	return &AuditBuilder{ctx: c}
}

func (b *ResponseBuilder) Ok(data interface{}) {
	b.JSON(data)
}

func (b *ResponseBuilder) Created(data interface{}) {
	b.Status(http.StatusCreated).JSON(data)
}

func (b *ResponseBuilder) Accepted(data interface{}) {
	b.Status(http.StatusAccepted).JSON(data)
}

func (b *ResponseBuilder) Auto(data interface{}) {
	b.execute(NewAutoResult(data).WithCode(b.statusCode()))
}

func (b *ResponseBuilder) JSON(data interface{}) {
	b.execute(JSONCode(data, b.statusCode()))
}

func (b *ResponseBuilder) XML(data interface{}) {
	b.execute(XMLCode(data, b.statusCode()))
}

func (b *ResponseBuilder) HTML(template string, data interface{}) {
	b.execute(HTMLCode(template, data, b.statusCode()))
}

func (b *ResponseBuilder) String(format string, values ...interface{}) {
	b.execute(StringCode(format, b.statusCode(), values...))
}

func (b *ResponseBuilder) Data(contentType string, data []byte) {
	b.execute(DataCode(contentType, data, b.statusCode()))
}

func (b *ResponseBuilder) Error(err error) {
	b.execute(ErrorCode(err, b.statusCode()))
}

func (b *ResponseBuilder) ErrorMsg(msg string) {
	b.execute(ErrorStatusCode(b.statusCode(), msg))
}

func (b *ResponseBuilder) Redirect(location string) {
	if b == nil || b.ctx == nil {
		return
	}
	b.apply()
	status := b.statusCode()
	if status == http.StatusOK {
		status = http.StatusFound
	}
	RedirectCode(location, status).Execute(b.ctx)
}

func (b *ResponseBuilder) File(path string) {
	b.execute(File(path))
}

func (b *ResponseBuilder) NoContent() {
	if b == nil || b.ctx == nil {
		return
	}
	b.code = http.StatusNoContent
	b.apply()
	b.ctx.Status(http.StatusNoContent)
}

func (b *ResponseBuilder) execute(result Result) {
	if b == nil || b.ctx == nil {
		return
	}
	b.apply()
	if result == nil {
		return
	}
	result.Execute(b.ctx)
}

func (b *ResponseBuilder) apply() {
	for key, value := range b.headers {
		b.ctx.Header(key, value)
	}
}

func (b *ResponseBuilder) statusCode() int {
	if b.code == 0 {
		return http.StatusOK
	}
	return b.code
}
