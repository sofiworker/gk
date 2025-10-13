package gclient

import (
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/http/httpguts"
)

func ValidMethod(method string) bool {
	/*
	     Method         = "OPTIONS"                ; Section 9.2
	                    | "GET"                    ; Section 9.3
	                    | "HEAD"                   ; Section 9.4
	                    | "POST"                   ; Section 9.5
	                    | "PUT"                    ; Section 9.6
	                    | "DELETE"                 ; Section 9.7
	                    | "TRACE"                  ; Section 9.8
	                    | "CONNECT"                ; Section 9.9
	                    | extension-method
	   extension-method = token
	     token          = 1*<any CHAR except CTLs or separators>
	*/
	return len(method) > 0 && strings.IndexFunc(method, IsNotToken) == -1
}

func IsNotToken(r rune) bool {
	return !httpguts.IsTokenRune(r)
}

func ConstructURL(baseurl, path string) (string, error) {
	pathURL, err := url.Parse(path)
	if err != nil {
		return "", ErrInvalidPath
	}

	if pathURL.IsAbs() {
		return pathURL.String(), nil
	}

	if baseurl == "" {
		return "", ErrBaseUrlEmpty
	}

	baseURL, err := url.Parse(baseurl)
	if err != nil {
		return "", ErrBaseUrlFormat
	}

	mergedURL := baseURL.ResolveReference(pathURL)

	if !mergedURL.IsAbs() {
		return "", ErrUrlNotAbs
	}

	return mergedURL.String(), nil
}

func IsValidURL(u string) bool {
	_, err := url.Parse(u)
	return err == nil
}

func CloneURLValues(v url.Values) url.Values {
	if v == nil {
		return nil
	}
	return url.Values(http.Header(v).Clone())
}
