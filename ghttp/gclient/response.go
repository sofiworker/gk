package gclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sofiworker/gk/ghttp/codec"
)

type Response struct {
	client *Client

	Request     *Request
	RawResponse *http.Response

	StatusCode    int
	Status        string
	Header        http.Header
	Body          []byte
	Duration      time.Duration
	Proto         string
	ContentType   string
	businessError error
}

func (r *Response) Bytes() []byte {
	if r == nil {
		return nil
	}
	return r.Body
}

func (r *Response) String() string {
	return string(r.Bytes())
}

func (r *Response) Reader() io.ReadCloser {
	if r == nil {
		return nil
	}
	return io.NopCloser(bytes.NewReader(r.Body))
}

func (r *Response) Len() int {
	if r == nil {
		return 0
	}
	return len(r.Body)
}

func (r *Response) IsSuccess() bool {
	if r == nil {
		return false
	}
	return r.StatusCode >= 200 && r.StatusCode < 300
}

func (r *Response) IsFailure() bool {
	if r == nil {
		return false
	}
	return r.StatusCode >= 400
}

func (r *Response) IsOK() bool {
	if r == nil {
		return false
	}
	return r.IsSuccess() && r.businessError == nil
}

func (r *Response) BusinessError() error {
	if r == nil {
		return nil
	}
	return r.businessError
}

func (r *Response) OK() error {
	if r == nil {
		return nil
	}
	if r.businessError != nil {
		return r.businessError
	}
	if !r.IsSuccess() {
		return &HTTPError{
			StatusCode: r.StatusCode,
			Message:    r.Status,
			Response:   r,
		}
	}
	return nil
}

func (r *Response) MustOK() *Response {
	if err := r.OK(); err != nil {
		panic(err)
	}
	return r
}

func (r *Response) HeaderGet(key string) string {
	if r == nil || r.Header == nil {
		return ""
	}
	return r.Header.Get(key)
}

func (r *Response) Cookies() []*http.Cookie {
	if r == nil {
		return nil
	}
	if r.RawResponse != nil {
		return r.RawResponse.Cookies()
	}
	if len(r.Header) == 0 {
		return nil
	}
	return (&http.Response{Header: r.Header.Clone()}).Cookies()
}

func (r *Response) Result() interface{} {
	if r == nil || r.Request == nil {
		return nil
	}
	return r.Request.Result
}

func (r *Response) ResultError() interface{} {
	if r == nil || r.Request == nil {
		return nil
	}
	return r.Request.ResultError
}

func (r *Response) Into(target interface{}) error {
	return r.Decode(target)
}

func (r *Response) UnmarshalJSON(target interface{}) error {
	return r.JSON(target)
}

func (r *Response) Unmarshal(target interface{}) error {
	return r.Decode(target)
}

func (r *Response) MustInto(target interface{}) {
	if err := r.Into(target); err != nil {
		panic(err)
	}
}

func (r *Response) MustJSON(target interface{}) {
	if err := r.JSON(target); err != nil {
		panic(err)
	}
}

func (r *Response) StatusText() string {
	if r == nil {
		return ""
	}
	if text := http.StatusText(r.StatusCode); text != "" {
		return text
	}
	if r.Status != "" {
		return r.Status
	}
	return ""
}

func (r *Response) ToHTTPResponse() *http.Response {
	if r == nil {
		return nil
	}
	if r.RawResponse != nil {
		cp := new(http.Response)
		*cp = *r.RawResponse
		cp.Header = r.Header.Clone()
		cp.Body = r.Reader()
		cp.ContentLength = int64(len(r.Body))
		return cp
	}

	resp := &http.Response{
		StatusCode:    r.StatusCode,
		Status:        r.Status,
		Proto:         r.Proto,
		Header:        r.Header.Clone(),
		Body:          r.Reader(),
		ContentLength: int64(len(r.Body)),
	}
	if resp.Status == "" && resp.StatusCode > 0 {
		resp.Status = fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	if r.Request != nil && r.Request.RawRequest != nil {
		resp.Request = r.Request.RawRequest
	}
	return resp
}

func ResponseFromHTTPResponse(httpResp *http.Response) (*Response, error) {
	if httpResp == nil {
		return nil, nil
	}
	rawResp := new(http.Response)
	*rawResp = *httpResp
	defer func() {
		_ = httpResp.Body.Close()
	}()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	return &Response{
		RawResponse: resetHTTPResponseBody(rawResp, body),
		StatusCode:  httpResp.StatusCode,
		Status:      httpResp.Status,
		Proto:       httpResp.Proto,
		Header:      httpResp.Header.Clone(),
		Body:        body,
		ContentType: httpResp.Header.Get(headerContentType),
	}, nil
}

func (r *Response) Decode(target interface{}) error {
	if r == nil {
		return nil
	}
	if target == nil {
		return nil
	}

	manager := codec.DefaultManager()
	if r.client != nil && r.client.codecManager != nil {
		manager = r.client.codecManager
	}
	if manager == nil {
		return json.Unmarshal(r.Body, target)
	}

	codec := manager.GetCodec(r.ContentType)
	if codec == nil {
		codec = manager.DefaultCodec()
	}
	if codec == nil {
		return json.Unmarshal(r.Body, target)
	}
	return codec.Decode(r.Body, target)
}

func (r *Response) JSON(target interface{}) error {
	if target == nil {
		return nil
	}
	return json.Unmarshal(r.Body, target)
}

func (r *Response) bindResult() error {
	if r == nil || r.Request == nil {
		return nil
	}

	if checker := r.statusChecker(); checker != nil {
		r.businessError = checker(r)
	}

	if len(r.Body) == 0 {
		return nil
	}
	if r.IsOK() && r.Request.Result != nil {
		if unwrapper := r.unwrapper(); unwrapper != nil {
			return unwrapper(r, r.Request.Result)
		}
		return r.Decode(r.Request.Result)
	}
	if !r.IsOK() && r.Request.ResultError != nil {
		return r.Decode(r.Request.ResultError)
	}
	return nil
}

func (r *Response) unwrapper() ResponseUnwrapper {
	if r == nil || r.Request == nil {
		return nil
	}
	if r.Request.responseUnwrapper != nil {
		return r.Request.responseUnwrapper
	}
	if r.client != nil {
		return r.client.responseUnwrapper
	}
	return nil
}

func (r *Response) statusChecker() ResponseStatusChecker {
	if r == nil || r.Request == nil {
		return nil
	}
	if r.Request.responseStatusChecker != nil {
		return r.Request.responseStatusChecker
	}
	if r.client != nil {
		return r.client.responseStatusChecker
	}
	return nil
}

func resetHTTPResponseBody(resp *http.Response, body []byte) *http.Response {
	if resp == nil {
		return nil
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	return resp
}
