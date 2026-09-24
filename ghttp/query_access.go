package ghttp

// QueryFirst 按需读取首个 query 值，保留缺失与空值区别；大量参数时回退到缓存映射。
// QueryFirst reads the first query value, distinguishing absence from empty; large queries use the cached map.
func (r *Request) QueryFirst(key string) (string, bool) {
	if r.queryCache == nil && r.useLazyQuery() {
		return lazyQueryFirst(r.URL.RawQuery, key)
	}
	values := r.Query()[key]
	if len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// QueryValues 按需读取全部同名 query 值；返回值不应由调用方修改。
// QueryValues reads all values of a query key; callers must not modify the returned slice.
func (r *Request) QueryValues(key string) []string {
	if r.queryCache == nil && r.useLazyQuery() {
		return lazyQueryAll(r.URL.RawQuery, key, nil)
	}
	return r.Query()[key]
}

func (r *Request) useLazyQuery() bool {
	if !r.queryCountKnown {
		r.querySmall = lazyQueryKeyCount(r.URL.RawQuery) <= lazyQueryMaxKeys
		r.queryCountKnown = true
	}
	return r.querySmall
}
