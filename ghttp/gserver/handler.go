package gserver

type Handler interface {
	Handle(ctx *Context)
}

// HandlerFunc 适配器
type HandlerFunc func(ctx *Context)

func (h HandlerFunc) Handle(ctx *Context) {
	h(ctx)
}
