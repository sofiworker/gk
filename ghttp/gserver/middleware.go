package gserver

func RequestLogger() HandlerFunc {
	return func(ctx *Context) {
	}
}

func Recovery() HandlerFunc {
	return func(ctx *Context) {
		defer func() {
			if rec := recover(); rec != nil {
				if ctx.Writer != nil && !ctx.Writer.Written() {
					//ctx.AbortWithStatusJSON(http.StatusInternalServerError, map[string]interface{}{
					//	"error": "internal server error",
					//})
				} else {
					ctx.Abort()
				}
			}
		}()

		ctx.Next()
	}
}
