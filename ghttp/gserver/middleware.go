package gserver

func Logger() HandlerFunc {
	return func(ctx *Context) {

	}
}

func Recovery() HandlerFunc {
	return func(ctx *Context) {
		if rec := recover(); rec != nil {

		}
	}
}
