package ghttp

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func baDo(t *testing.T, mw Middleware, user, pass string, set bool) (*httptest.ResponseRecorder, string, error) {
	t.Helper()
	r := httptest.NewRequest("GET", "/", nil)
	if set {
		r.SetBasicAuth(user, pass)
	}
	rec := httptest.NewRecorder()
	var gotUser string
	h := mw(func(ctx context.Context, req *Request, _ *Response) error {
		gotUser, _ = BasicAuthUser(ctx)
		if u, ok := BasicAuthUser(req.Context()); !ok || u != gotUser {
			t.Error("request context missing user")
		}
		return nil
	})
	err := h(r.Context(), &Request{Raw: r}, &Response{Writer: rec})
	return rec, gotUser, err
}

func TestBasicAuth(t *testing.T) {
	mw := BasicAuth(BasicAuthAccounts(map[string]string{"alice": "pw"}), WithBasicAuthRealm("a\"b\r\nc\\"))
	_, u, err := baDo(t, mw, "alice", "pw", true)
	if err != nil || u != "alice" {
		t.Fatalf("err=%v user=%q", err, u)
	}
	for _, c := range []struct {
		u, p string
		set  bool
	}{{"alice", "bad", true}, {"bob", "pw", true}, {"", "", false}, {"alice", "", true}} {
		rec, _, err := baDo(t, mw, c.u, c.p, c.set)
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("%+v err=%v", c, err)
		}
		if got, want := rec.Header().Get("WWW-Authenticate"), `Basic realm="abc", charset="UTF-8"`; got != want {
			t.Fatalf("header %q want %q", got, want)
		}
	}
}

func TestBasicAuth_DefaultRealmAndNilValidate(t *testing.T) {
	rec, _, err := baDo(t, BasicAuth(nil), "a", "b", true)
	if !errors.Is(err, ErrUnauthorized) || !strings.Contains(rec.Header().Get("WWW-Authenticate"), `realm="Restricted"`) {
		t.Fatal(err, rec.Header())
	}
}

func TestBasicAuthAccounts_Copy(t *testing.T) {
	m := map[string]string{"a": "1"}
	v := BasicAuthAccounts(m)
	m["a"] = "2"
	if !v(context.Background(), "a", "1") || v(context.Background(), "a", "2") {
		t.Fatal("accounts not copied")
	}
}
