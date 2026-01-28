package gserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valyala/fasthttp"
)

func TestServerSetCookie(t *testing.T) {
	s := NewServer()
	s.GET("/cookie", func(c *Context) {
		ck := &fasthttp.Cookie{}
		ck.SetKey("a")
		ck.SetValue("b")
		c.SetCookie(ck)
		c.String(http.StatusOK, "ok")
	})

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/cookie", nil))
	setCookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "a=b") {
		t.Fatalf("expected Set-Cookie to contain a=b, got %q", setCookie)
	}
}
