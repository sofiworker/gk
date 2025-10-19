package gserver

import (
	"net/http"
	"runtime/debug"
	"time"
)

func RequestLogger() HandlerFunc {
	return func(ctx *Context) {
		if ctx == nil {
			return
		}
		start := time.Now()
		ctx.Next()

		engine := ctx.Engine()
		if engine == nil {
			return
		}
		logger := engine.Logger()
		if logger == nil {
			return
		}

		var (
			method = ""
			path   = ""
		)
		if ctx.Request != nil {
			method = ctx.Request.Method
			path = ctx.Request.URL.RequestURI()
		}

		logger.Infof("%s %s -> %d (%s)", method, path, ctx.StatusCode(), time.Since(start))
	}
}

func Recovery() HandlerFunc {
	return func(ctx *Context) {
		defer func() {
			if rec := recover(); rec != nil {
				engine := ctx.Engine()
				if engine != nil && engine.panicHandler != nil {
					engine.panicHandler(ctx, rec)
					return
				}

				if engine != nil && engine.Logger() != nil {
					engine.Logger().Errorf("panic recovered: %v\n%s", rec, debug.Stack())
				}

				if ctx.Writer != nil && !ctx.Writer.Written() {
					ctx.AbortWithStatusJSON(http.StatusInternalServerError, map[string]interface{}{
						"error": "internal server error",
					})
				} else {
					ctx.Abort()
				}
			}
		}()

		ctx.Next()
	}
}
