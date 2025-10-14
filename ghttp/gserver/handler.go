package gserver

type Handler interface {
}

type HandlerFunc func(ctx *Context)
