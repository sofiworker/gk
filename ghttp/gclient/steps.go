package gclient

import (
	"net/http"
	"net/url"
	"time"
)

func WithHeader(key, value string) RequestStep {
	return func(req *Request) error {
		req.SetHeader(key, value)
		return nil
	}
}

func WithHeaders(headers map[string]string) RequestStep {
	return func(req *Request) error {
		req.SetHeaders(headers)
		return nil
	}
}

func WithQuery(key, value string) RequestStep {
	return func(req *Request) error {
		req.SetQueryParam(key, value)
		return nil
	}
}

func WithQueryValues(values url.Values) RequestStep {
	return func(req *Request) error {
		req.AddQueryValues(values)
		return nil
	}
}

func WithPathParam(key, value string) RequestStep {
	return func(req *Request) error {
		req.SetPathParam(key, value)
		return nil
	}
}

func WithJSONBody(body interface{}) RequestStep {
	return func(req *Request) error {
		req.SetJSONBody(body)
		return nil
	}
}

func WithResult(result interface{}) RequestStep {
	return func(req *Request) error {
		req.SetResult(result)
		return nil
	}
}

func WithResultError(result interface{}) RequestStep {
	return func(req *Request) error {
		req.SetResultError(result)
		return nil
	}
}

func WithBearerToken(token string) RequestStep {
	return func(req *Request) error {
		req.SetBearerToken(token)
		return nil
	}
}

func WithBasicAuth(username, password string) RequestStep {
	return func(req *Request) error {
		req.SetBasicAuth(username, password)
		return nil
	}
}

func WithTimeout(timeout time.Duration) RequestStep {
	return func(req *Request) error {
		req.SetTimeout(timeout)
		return nil
	}
}

func WithMethod(method string) RequestStep {
	return func(req *Request) error {
		req.SetMethod(method)
		return nil
	}
}

func WithURL(rawURL string) RequestStep {
	return func(req *Request) error {
		req.SetURL(rawURL)
		return nil
	}
}

func WithCookie(cookie *http.Cookie) RequestStep {
	return func(req *Request) error {
		req.SetCookie(cookie)
		return nil
	}
}
