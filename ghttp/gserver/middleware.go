package gserver

import (
	"net/http"
	"runtime/debug"
	"time"
)

func RequestLogger() HandlerFunc {
	return func(ctx *Context) {
		start := time.Now()
		ctx.Next()

		logger := ctx.Logger()
		if logger == nil {
			return
		}

		method := ""
		path := ""
		if ctx.fastCtx != nil {
			method = string(ctx.fastCtx.Method())
			path = string(ctx.fastCtx.Path())
		}

		status := http.StatusOK
		if ctx.Writer != nil {
			status = ctx.Writer.Status()
		}
		logger.Infof("request %s %s -> %d (%s)", method, path, status, time.Since(start))
	}
}

func Recovery() HandlerFunc {
	return func(ctx *Context) {
		defer func() {
			if rec := recover(); rec != nil {
				if logger := ctx.Logger(); logger != nil {
					logger.Errorf("panic recovered: %v\n%s", rec, string(debug.Stack()))
				}

				if ctx.Writer != nil && !ctx.Writer.Written() {
					ctx.Status(http.StatusInternalServerError)
				}
				ctx.Abort()
			}
		}()

		ctx.Next()
	}
}
