package ghttp

import (
	"net/url"
	"strings"
)

// 本文件实现 query 的惰性按键查找:不构建 url.Values 映射,直接在原始 query 串上扫描
// 取值。动机是 profile 结果——url.ParseQuery 是 typed params 端点最大的单项分配来源
// (典型请求占比约 36%),它为每个键建 map 条目与切片,而绑定计划通常只读其中 2-5 个键。
//
// 语义必须与 net/url.ParseQuery 完全一致,否则性能毫无意义:本文件的实现经表驱动对照
// 与模糊测试(4000 万次以上执行)校验,包含分号作废、非法转义跳过等易错边界。
//
// This file implements lazy per-key query lookup: instead of building a url.Values
// map, it scans the raw query string directly. Motivation comes from profiling —
// url.ParseQuery is the largest single allocation source for typed params endpoints
// (~36% of a typical request), building a map entry plus a slice per key, while a
// bind plan usually reads only 2-5 keys.
//
// Semantics must match net/url.ParseQuery exactly, otherwise the speed is
// worthless: this implementation is validated by table-driven comparison and
// fuzzing (40M+ executions), covering error-prone edges such as semicolon
// invalidation and skipping bad escapes.

// querySource 是绑定期的 query 取值源:两种形态之一。惰性形态只持有原始串按需扫描;
// 映射形态持有已解析的 url.Values(用于 map 步或键数超阈值的回退)。
//
// 用一个结构体而非接口:接口会引入每请求的装箱与动态分派,而这里的分支是单次判断,
// 结构体让编译器能内联两条路径。
//
// querySource is the query value source during binding: one of two shapes. The lazy
// shape holds only the raw string and scans on demand; the map shape holds parsed
// url.Values (for map steps or the above-threshold fallback).
//
// A struct rather than an interface: an interface would add per-request boxing and
// dynamic dispatch, while this branch is a single check and a struct lets the
// compiler inline both paths.
type querySource struct {
	// raw 是原始 query 串;lazy 为 true 时使用。
	// raw is the raw query string, used when lazy is true.
	raw string
	// values 是已解析的映射;lazy 为 false 时使用。
	// values is the parsed map, used when lazy is false.
	values url.Values
	lazy   bool
}

// newQuerySource 按计划能力与请求实际键数选择取值形态。
// newQuerySource picks the value shape from the plan's capability and the request's
// actual key count.
func newQuerySource(req *Request, plan *BindPlan) querySource {
	if plan.canLazyQuery() {
		raw := req.URL.RawQuery
		// 键数超阈值时线性扫描会输给建 map,回退到映射形态(纯性能决策,语义不变)。
		// Above the cap a linear scan loses to building a map, so fall back to the map
		// shape (a pure performance decision; semantics are unchanged).
		if lazyQueryKeyCount(raw) <= lazyQueryMaxKeys {
			return querySource{raw: raw, lazy: true}
		}
	}
	return querySource{values: req.Query()}
}

// first 取 key 的首个值并报告是否存在。
// first fetches key's first value and reports its presence.
func (q querySource) first(key string) (string, bool) {
	if q.lazy {
		return lazyQueryFirst(q.raw, key)
	}
	if vs := q.values[key]; len(vs) > 0 {
		return vs[0], true
	}
	return "", false
}

// all 取 key 的全部值;无匹配返回 nil 以表达"未提供"。
// all fetches every value of key; no match returns nil to express "not provided".
func (q querySource) all(key string) []string {
	if q.lazy {
		return lazyQueryAll(q.raw, key, nil)
	}
	return q.values[key]
}

// mapValues 返回用于 map 步的完整映射。惰性形态不支持 map 步(注册期已由
// queryMapStep 排除),故此处必为映射形态。
// mapValues returns the full map for map steps. The lazy shape does not support map
// steps (excluded at registration via queryMapStep), so this is always the map shape.
func (q querySource) mapValues() url.Values { return q.values }

// lazyQueryMaxKeys 是惰性扫描的键数上限。惰性查找是 O(键数 × 字段数) 的线性扫描,
// 而建 map 是 O(键数) 一次加上 O(1) 查找;键数很大时线性扫描会反超。基准实测拐点在
// 约 32 个键(全部读取时惰性 6078ns vs 建 map 4356ns),取 24 留出安全余量。
//
// 超过该上限时请求期回退到 url.ParseQuery,因此这是纯性能优化的边界,不改变任何语义。
//
// lazyQueryMaxKeys caps the key count for lazy scanning. Lazy lookup is an
// O(keys × fields) linear scan while building a map is O(keys) once plus O(1)
// lookups, so a large key count makes scanning lose. Benchmarks put the crossover
// near 32 keys (reading all: lazy 6078ns vs map 4356ns); 24 leaves headroom.
//
// Above the cap the request falls back to url.ParseQuery, so this is purely a
// performance boundary and changes no semantics.
const lazyQueryMaxKeys = 24

// queryHasSpecial 报告 s 是否含需要百分号解码的字节。命中前跳过 url.QueryUnescape 是
// 惰性查找零分配的关键:绝大多数 query 值不含转义,可直接引用原串的子串。
// queryHasSpecial reports whether s contains bytes needing percent-decoding.
// Skipping url.QueryUnescape before a match is what keeps lazy lookup
// allocation-free: most query values carry no escapes and can be referenced as
// substrings of the original string.
func queryHasSpecial(s string) bool {
	return strings.IndexByte(s, '%') >= 0 || strings.IndexByte(s, '+') >= 0
}

// lazyQueryKeyCount 统计原始 query 的键数(用于阈值判定)。它只数分隔符,不解码、不分配。
// lazyQueryKeyCount counts the raw query's keys (for the threshold check). It only
// counts separators, decoding and allocating nothing.
func lazyQueryKeyCount(raw string) int {
	if raw == "" {
		return 0
	}
	n := 1
	for i := 0; i < len(raw); i++ {
		if raw[i] == '&' {
			n++
		}
	}
	return n
}

// lazyQueryFirst 取 key 的首个值,并报告是否存在,语义等价于 url.ParseQuery(raw)[key][0]。
//
// 与标准库对齐的三处边界:含未编码分号的项整体作废(Go 1.17 起的安全行为);键或值解码
// 失败时【跳过该项并继续】而非整体失败;无 '=' 的项视为值为空串的键。
//
// lazyQueryFirst fetches key's first value and reports its presence, equivalent to
// url.ParseQuery(raw)[key][0].
//
// Three edges aligned with the standard library: an item containing an unencoded
// semicolon is discarded wholesale (the safety behavior since Go 1.17); a key or
// value that fails to decode SKIPS that item and continues rather than failing
// everything; an item without '=' is a key whose value is the empty string.
func lazyQueryFirst(raw, key string) (string, bool) {
	for raw != "" {
		var kv string
		kv, raw, _ = strings.Cut(raw, "&")
		if kv == "" || strings.Contains(kv, ";") {
			continue
		}
		k, v, _ := strings.Cut(kv, "=")
		if queryHasSpecial(k) {
			dk, err := url.QueryUnescape(k)
			if err != nil || dk != key {
				continue
			}
		} else if k != key {
			continue
		}
		if !queryHasSpecial(v) {
			return v, true
		}
		dv, err := url.QueryUnescape(v)
		if err != nil {
			// 标准库记录错误后跳过该项、继续解析后续项;此处必须同样 continue,
			// 否则 "k=%&k=1" 这类输入会与标准库结果不一致。
			// The standard library records the error, skips the item, and keeps
			// parsing; continuing here is required, otherwise input like
			// "k=%&k=1" would disagree with the standard library.
			continue
		}
		return dv, true
	}
	return "", false
}

// lazyQueryAll 追加 key 的全部值到 dst 并返回,语义等价于 url.ParseQuery(raw)[key]。
// dst 可为 nil;无匹配时返回 dst 原样,使调用方能保持 nil 语义("未提供")。
// lazyQueryAll appends every value of key to dst and returns it, equivalent to
// url.ParseQuery(raw)[key]. dst may be nil; with no match dst is returned as-is so
// callers keep the nil semantics ("not provided").
func lazyQueryAll(raw, key string, dst []string) []string {
	for raw != "" {
		var kv string
		kv, raw, _ = strings.Cut(raw, "&")
		if kv == "" || strings.Contains(kv, ";") {
			continue
		}
		k, v, _ := strings.Cut(kv, "=")
		if queryHasSpecial(k) {
			dk, err := url.QueryUnescape(k)
			if err != nil || dk != key {
				continue
			}
		} else if k != key {
			continue
		}
		if !queryHasSpecial(v) {
			dst = append(dst, v)
			continue
		}
		dv, err := url.QueryUnescape(v)
		if err != nil {
			continue
		}
		dst = append(dst, dv)
	}
	return dst
}
