package ghttp

import (
	"bytes"
	"github.com/valyala/fasthttp"
	"reflect"
	"testing"
)

func newTestRequest() *Request {
	client := &Client{
		fastClient: &fasthttp.Client{},
	}
	return &Request{
		fr:     fasthttp.AcquireRequest(),
		Client: client,
	}
}

func TestSetBearToken(t *testing.T) {
	req := newTestRequest()
	token := "testtoken"
	req.SetBearToken(token)
	got := string(req.fr.Header.Peek("Authorization"))
	want := "Bearer testtoken"
	if got != want {
		t.Errorf("Authorization header = %v, want %v", got, want)
	}
}

func TestSetJsonBody(t *testing.T) {
	req := newTestRequest()
	data := map[string]string{"foo": "bar"}
	req.SetJsonBody(data)
	if !reflect.DeepEqual(req.requestBody, data) {
		t.Errorf("requestBody = %v, want %v", req.requestBody, data)
	}
}

func TestSetUnmarshalData(t *testing.T) {
	req := newTestRequest()
	var result map[string]interface{}
	req.SetUnmarshalData(&result)
	if req.returnData != &result {
		t.Errorf("returnData not set correctly")
	}
}

func TestSetUrl(t *testing.T) {
	req := newTestRequest()
	url := "/api/test"
	req.SetUrl(url)
	if req.url != url {
		t.Errorf("url = %v, want %v", req.url, url)
	}
}

func TestSetMethod(t *testing.T) {
	req := newTestRequest()
	method := "POST"
	req.SetMethod(method)
	if req.method != method {
		t.Errorf("method = %v, want %v", req.method, method)
	}
}

func TestSetContentType(t *testing.T) {
	req := newTestRequest()
	ct := "application/json"
	req.SetContentType(ct)
	got := string(req.fr.Header.Peek("Content-Type"))
	if got != ct {
		t.Errorf("Content-Type = %v, want %v", got, ct)
	}
}

func TestSetEnableDumpBody(t *testing.T) {
	req := newTestRequest()
	req.SetEnableDumpBody(true)
	if !req.enableDumpBody {
		t.Errorf("enableDumpBody should be true")
	}
}

func TestUploadFile(t *testing.T) {
	req := newTestRequest()
	path := "/tmp/test.txt"
	req.UploadFile(path)
	if req.file != path {
		t.Errorf("file = %v, want %v", req.file, path)
	}
}

func TestUploadFileByReader(t *testing.T) {
	req := newTestRequest()
	reader := &bytes.Buffer{}
	req.UploadFileByReader(reader)
	if req.fileReader != reader {
		t.Errorf("fileReader not set correctly")
	}
}

func TestUploadFileByReaderWithSize(t *testing.T) {
	req := newTestRequest()
	reader := &bytes.Buffer{}
	size := 123
	req.UploadFileByReaderWithSize(reader, size)
	if req.fileReader != reader || req.fileReaderSize != size {
		t.Errorf("fileReader or fileReaderSize not set correctly")
	}
}
