package ghttp

import (
	"net/http"
	"time"
)

// CookieWriter allows typed responses to write Set-Cookie headers.
type CookieWriter interface {
	Cookies() []*http.Cookie
}

// DeleteCookie returns a cookie that deletes the named cookie on the default path.
func DeleteCookie(name string) *http.Cookie {
	return &http.Cookie{
		Name:    name,
		Path:    "/",
		Expires: time.Unix(0, 0).UTC(),
		MaxAge:  -1,
	}
}

func writeResponseCookies(w http.ResponseWriter, resp interface{}) {
	cookieWriter, ok := resp.(CookieWriter)
	if !ok {
		return
	}
	for _, cookie := range cookieWriter.Cookies() {
		if cookie == nil {
			continue
		}
		http.SetCookie(w, cookie)
	}
}
