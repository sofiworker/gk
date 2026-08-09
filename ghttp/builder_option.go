package ghttp

// RouteOption 以函数式选项配置路由构建器，由 Apply 批量应用。
// RouteOption configures the route builder as a functional option, applied by Apply in bulk.
// 与链式方法共享同一组 setter；终结后调用由 ensureMutable 的 finalize 守卫兜住。
// It shares the same setters as the chain methods; the finalized guard rejects late calls.
type RouteOption func(*routeBuilderCore)

// GroupOptions 把多个选项打包为一个可复用组合选项，便于跨路由共享配置。
// GroupOptions bundles options into one reusable option for cross-route reuse.
func GroupOptions(opts ...RouteOption) RouteOption {
	return func(c *routeBuilderCore) {
		for _, opt := range opts {
			if opt != nil {
				opt(c)
			}
		}
	}
}

// OptDoc 等价于链式 Doc。
// OptDoc is equivalent to the chained Doc.
func OptDoc(opts ...DocOption) RouteOption {
	return func(c *routeBuilderCore) { c.docOptions(opts...) }
}

// OptProduces 等价于链式 Produces。
// OptProduces is equivalent to the chained Produces.
func OptProduces(contentTypes ...string) RouteOption {
	return func(c *routeBuilderCore) { c.setProduces(contentTypes...) }
}

// OptConsumes 等价于链式 Consumes。
// OptConsumes is equivalent to the chained Consumes.
func OptConsumes(contentTypes ...string) RouteOption {
	return func(c *routeBuilderCore) { c.setConsumes(contentTypes...) }
}

// OptMaxBodyBytes 等价于链式 MaxBodyBytes。
// OptMaxBodyBytes is equivalent to the chained MaxBodyBytes.
func OptMaxBodyBytes(n int64) RouteOption {
	return func(c *routeBuilderCore) { c.setMaxBodyBytes(n) }
}

// OptUse 等价于链式 Use。
// OptUse is equivalent to the chained Use.
func OptUse(mws ...Middleware) RouteOption {
	return func(c *routeBuilderCore) { c.use(mws...) }
}

// OptStatus 等价于链式 Status。
// OptStatus is equivalent to the chained Status.
func OptStatus(code int) RouteOption {
	return func(c *routeBuilderCore) { c.status(code) }
}

// OptResponseHeader 等价于链式 ResponseHeader。
// OptResponseHeader is equivalent to the chained ResponseHeader.
func OptResponseHeader(name, value string) RouteOption {
	return func(c *routeBuilderCore) { c.responseHeader(name, value) }
}

// OptErrorWriter 等价于链式 ErrorWriter。
// OptErrorWriter is equivalent to the chained ErrorWriter.
func OptErrorWriter(writer ErrorWriter) RouteOption {
	return func(c *routeBuilderCore) { c.setErrorWriter(writer) }
}

// OptProblemDetails 等价于链式 ProblemDetails。
// OptProblemDetails is equivalent to the chained ProblemDetails.
func OptProblemDetails() RouteOption {
	return func(c *routeBuilderCore) { c.problemDetails() }
}

// OptSkipValidation 等价于链式 SkipValidation。
// OptSkipValidation is equivalent to the chained SkipValidation.
func OptSkipValidation() RouteOption {
	return func(c *routeBuilderCore) { c.skipValidationSet() }
}

// OptValidate 等价于链式 Validate。
// OptValidate is equivalent to the chained Validate.
func OptValidate(fn any, opts ...ValidateOption) RouteOption {
	return func(c *routeBuilderCore) {
		c.ensureMutable()
		c.validator = fn
		var cfg validateOptions
		for _, opt := range opts {
			if opt != nil {
				opt(&cfg)
			}
		}
		c.validationError = cfg.err
	}
}
