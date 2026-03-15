package gclient

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var regexCmdQuote = regexp.MustCompile(`[^\w@%+=:,./-]`)

func (r *Request) CURL() (string, error) {
	httpReq, err := r.BuildHTTPRequest()
	if err != nil {
		return "", err
	}
	return buildCurlCmd(httpReq)
}

func (r *Request) MustCURL() string {
	curl, err := r.CURL()
	if err != nil {
		panic(err)
	}
	return curl
}

func buildCurlCmd(req *http.Request) (string, error) {
	if req == nil {
		return "curl", nil
	}

	var builder strings.Builder
	builder.WriteString("curl -X ")
	builder.WriteString(req.Method)

	headers := dumpCurlHeaders(req.Header)
	for _, header := range headers {
		builder.WriteString(" -H ")
		builder.WriteString(cmdQuote(header[0] + ": " + header[1]))
	}

	if len(req.Cookies()) > 0 {
		builder.WriteString(" -H ")
		builder.WriteString(cmdQuote(dumpCurlCookies(req.Cookies())))
	}

	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return "", err
		}
		req.Body = io.NopCloser(strings.NewReader(string(body)))
		if len(body) > 0 {
			builder.WriteString(" -d ")
			builder.WriteString(cmdQuote(strings.TrimRight(string(body), "\n")))
		}
	}

	builder.WriteString(" ")
	builder.WriteString(cmdQuote(req.URL.String()))
	return builder.String(), nil
}

func dumpCurlHeaders(header http.Header) [][2]string {
	headers := make([][2]string, 0, len(header))
	keys := make([]string, 0, len(header))
	for key := range header {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range header.Values(key) {
			headers = append(headers, [2]string{key, value})
		}
	}
	return headers
}

func dumpCurlCookies(cookies []*http.Cookie) string {
	if len(cookies) == 0 {
		return ""
	}
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		parts = append(parts, cookie.Name+"="+url.QueryEscape(cookie.Value))
	}
	return "Cookie: " + strings.Join(parts, "; ")
}

func cmdQuote(s string) string {
	if len(s) == 0 {
		return "''"
	}
	if regexCmdQuote.MatchString(s) {
		return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
	}
	return s
}
