package ghttp

import (
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestQueryAccessMatchesStandard(t *testing.T) {
	for _, raw := range []string{"", "k=1&k=2", "k=&x=2", "k=%&k=ok", "k=a;b&k=c", "%6b=a+b", "k=%E4%B8%AD", strings.Repeat("x=1&", 30) + "k=last"} {
		for _, cached := range []bool{false, true} {
			req := &Request{Request: httptest.NewRequest("GET", "/?"+raw, nil)}
			if cached {
				req.Query()
			}
			expected, _ := url.ParseQuery(raw)
			for _, key := range []string{"k", "x", "missing"} {
				all := req.QueryValues(key)
				if !reflect.DeepEqual(all, expected[key]) {
					t.Fatalf("%q %q: %v != %v", raw, key, all, expected[key])
				}
				value, ok := req.QueryFirst(key)
				if ok != (len(expected[key]) > 0) || ok && value != expected[key][0] {
					t.Fatalf("first %q %q: %q %v", raw, key, value, ok)
				}
			}
		}
	}
}
