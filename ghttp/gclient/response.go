package gclient

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/sofiworker/gk/ghttp/codec"
)

type Response struct {
	client *Client

	Request *Request

	StatusCode  int
	Status      string
	Header      http.Header
	Body        []byte
	Duration    time.Duration
	Proto       string
	ContentType string
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

func (r *Response) IsSuccess() bool {
	if r == nil {
		return false
	}
	return r.StatusCode >= 200 && r.StatusCode < 300
}

func (r *Response) HeaderGet(key string) string {
	if r == nil || r.Header == nil {
		return ""
	}
	return r.Header.Get(key)
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
