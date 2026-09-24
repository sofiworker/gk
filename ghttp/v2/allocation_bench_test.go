package v2

import (
	"net/url"
	"testing"
)

var queryAllocationSink url.Values

// 单独测量相同 query 的 map 物化成本，不缓存或省略解析。
// Measure map materialization for the same query without caching or skipping parsing.
func BenchmarkQueryAllocation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		queryAllocationSink, _ = url.ParseQuery("page=2&filter=golang")
	}
}
