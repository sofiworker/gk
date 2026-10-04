package v4

// RequestOf 包装原始请求，提供类型化的延迟访问。
// RequestOf wraps the raw request and provides typed lazy access.
//
// 与 v2/v3 不同，v4 的 RequestOf 不直接持有 Data 字段，
// 而是提供多个 Accessor 来分别访问不同来源的参数。
//
// Unlike v2/v3, v4's RequestOf doesn't hold a Data field directly,
// but provides multiple Accessors to access parameters from different sources.
type RequestOf[Path, Query, Body any] struct {
	Raw   *Request
	Path  *PathAccessor[Path]
	Query *QueryAccessor[Query]
	Body  *JsonAccessor[Body]
	Form  *FormAccessor[Body]
}

// NewRequestOf 创建 RequestOf 实例。
// NewRequestOf creates a RequestOf instance.
func NewRequestOf[Path, Query, Body any](r *Request) RequestOf[Path, Query, Body] {
	return RequestOf[Path, Query, Body]{
		Raw:   r,
		Path:  &PathAccessor[Path]{req: r},
		Query: &QueryAccessor[Query]{req: r},
		Body:  &JsonAccessor[Body]{req: r},
		Form:  &FormAccessor[Body]{req: r},
	}
}

// RequestOfMulti 是常用的 Path + Body 组合（简化版）。
// RequestOfMulti is the common Path + Body combination (simplified version).
type RequestOfMulti[Path, Body any] struct {
	Raw  *Request
	Path *PathAccessor[Path]
	Body *JsonAccessor[Body]
	Form *FormAccessor[Body]
}

// NewRequestOfMulti 创建 RequestOfMulti 实例。
// NewRequestOfMulti creates a RequestOfMulti instance.
func NewRequestOfMulti[Path, Body any](r *Request) RequestOfMulti[Path, Body] {
	return RequestOfMulti[Path, Body]{
		Raw:  r,
		Path: &PathAccessor[Path]{req: r},
		Body: &JsonAccessor[Body]{req: r},
		Form: &FormAccessor[Body]{req: r},
	}
}

// RequestWithQuery 是常用的 Query + Body 组合。
// RequestWithQuery is the common Query + Body combination.
type RequestWithQuery[Query, Body any] struct {
	Raw   *Request
	Query *QueryAccessor[Query]
	Body  *JsonAccessor[Body]
}

// NewRequestWithQuery 创建 RequestWithQuery 实例。
// NewRequestWithQuery creates a RequestWithQuery instance.
func NewRequestWithQuery[Query, Body any](r *Request) RequestWithQuery[Query, Body] {
	return RequestWithQuery[Query, Body]{
		Raw:   r,
		Query: &QueryAccessor[Query]{req: r},
		Body:  &JsonAccessor[Body]{req: r},
	}
}

// RequestSimple 是最简单的单一 Body 组合。
// RequestSimple is the simplest single Body combination.
type RequestSimple[Body any] struct {
	Raw  *Request
	Body *JsonAccessor[Body]
}

// NewRequestSimple 创建 RequestSimple 实例。
// NewRequestSimple creates a RequestSimple instance.
func NewRequestSimple[Body any](r *Request) RequestSimple[Body] {
	return RequestSimple[Body]{
		Raw:  r,
		Body: &JsonAccessor[Body]{req: r},
	}
}
