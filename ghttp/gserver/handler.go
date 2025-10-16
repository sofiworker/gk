package gserver

// HandlerFunc 适配器
type HandlerFunc func(ctx *Context)

type Result interface {
	Execute(c *Context)
}

type ResultHandler func(*Context) Result

func Wrap(handler ResultHandler) HandlerFunc {
	return func(ctx *Context) {
		result := handler(ctx)
		if result != nil {
			result.Execute(ctx)
		}
	}
}

func Wraps(handler ...ResultHandler) []HandlerFunc {
	handlers := make([]HandlerFunc, len(handler))
	for i, h := range handler {
		handlers[i] = Wrap(h)
	}
	return handlers
}
