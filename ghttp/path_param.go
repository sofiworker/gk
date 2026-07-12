package ghttp

import "net/http"

const maxStackPathParams = 16

type pathParam struct {
	Key   string
	Value string
}

type pathParamList struct {
	values   [maxStackPathParams]pathParam
	overflow []pathParam
	len      int
}

func (ps pathParamList) Get(key string) string {
	for i := 0; i < ps.len; i++ {
		if param := ps.values[i]; param.Key == key {
			return param.Value
		}
	}
	for _, param := range ps.overflow {
		if param.Key == key {
			return param.Value
		}
	}
	return ""
}

func (ps pathParamList) Len() int {
	return ps.len + len(ps.overflow)
}

func (ps pathParamList) Clone() pathParamList {
	cloned := pathParamList{len: ps.len}
	copy(cloned.values[:], ps.values[:])
	if len(ps.overflow) > 0 {
		cloned.overflow = append([]pathParam(nil), ps.overflow...)
	}
	return cloned
}

func (ps *pathParamList) Add(key, value string) {
	if ps.len < len(ps.values) {
		ps.values[ps.len] = pathParam{Key: key, Value: value}
		ps.len++
		return
	}
	ps.overflow = append(ps.overflow, pathParam{Key: key, Value: value})
}

func (ps *pathParamList) Reset() {
	for i := 0; i < ps.len; i++ {
		ps.values[i] = pathParam{}
	}
	ps.len = 0
	ps.overflow = ps.overflow[:0]
}

func (ps *pathParamList) Truncate(n int) {
	if n <= len(ps.values) {
		for i := n; i < ps.len; i++ {
			ps.values[i] = pathParam{}
		}
		ps.len = n
		ps.overflow = ps.overflow[:0]
		return
	}
	ps.len = len(ps.values)
	ps.overflow = ps.overflow[:n-len(ps.values)]
}

type pathParamHandler interface {
	http.Handler
	ServeHTTPWithPathParams(http.ResponseWriter, *http.Request, pathParamList)
}

type pathParamHandlerFunc func(http.ResponseWriter, *http.Request, pathParamList)

func (f pathParamHandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f(w, r, pathParamList{})
}

func (f pathParamHandlerFunc) ServeHTTPWithPathParams(w http.ResponseWriter, r *http.Request, params pathParamList) {
	f(w, r, params)
}
