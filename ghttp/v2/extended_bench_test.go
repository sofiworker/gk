package v2

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	root "github.com/sofiworker/gk/ghttp"
)

type writeMethodInput struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type writeMethodOutput struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type writeMethodWriter struct {
	header http.Header
	status int
}

func (w *writeMethodWriter) Header() http.Header { return w.header }
func (w *writeMethodWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return len(p), nil
}
func (w *writeMethodWriter) WriteHeader(status int) { w.status = status }

func writeMethodServer(version, method string) (*root.Server, error) {
	s := root.New()
	h := func(_ context.Context, in writeMethodInput) (writeMethodOutput, error) {
		return writeMethodOutput{Name: in.Name, Age: in.Age}, nil
	}
	var err error
	switch version {
	case "v1":
		switch method {
		case http.MethodPost:
			err = root.PostBody(s, "/resource", root.JSONBody[writeMethodInput](), root.JSON[writeMethodOutput](), h)
		case http.MethodPut:
			err = root.PutBody(s, "/resource", root.JSONBody[writeMethodInput](), root.JSON[writeMethodOutput](), h)
		case http.MethodPatch:
			err = root.PatchBody(s, "/resource", root.JSONBody[writeMethodInput](), root.JSON[writeMethodOutput](), h)
		case http.MethodDelete:
			err = root.DeleteBody(s, "/resource", root.JSONBody[writeMethodInput](), root.JSON[writeMethodOutput](), h)
		}
	case "v2":
		err = Method(method, "/resource", h).Mount(s)
	case "v2DecodeWith":
		decode := DecodeWith(func(_ context.Context, req *Request) (writeMethodInput, error) {
			var in writeMethodInput
			decoder := json.NewDecoder(req.Body)
			if err := decoder.Decode(&in); err != nil {
				return in, err
			}
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF {
				if err == nil {
					err = errors.New("multiple JSON values")
				}
				return in, err
			}
			return in, nil
		})
		err = Method(method, "/resource", h, WithInput(decode)).Mount(s)
	}
	return s, err
}

// 复用请求时清理解析缓存和已写响应头，确保每次都重新解码请求体。
// Clear parsed request caches and response headers so reused requests decode the body every time.
func resetWriteMethodRequest(req *http.Request, body string, w *writeMethodWriter) {
	req.Body = io.NopCloser(strings.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Form = nil
	req.PostForm = nil
	req.MultipartForm = nil
	clear(w.header)
	w.status = 0
}

func TestWriteMethodsJSONContract(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, version := range []string{"v1", "v2", "v2DecodeWith"} {
			t.Run(method+"/"+version, func(t *testing.T) {
				s, err := writeMethodServer(version, method)
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(method, "/resource", nil)
				req.Header.Set("Content-Type", "application/json")
				for _, tc := range []struct {
					body string
					want writeMethodOutput
				}{
					{`{"name":"alice","age":30}`, writeMethodOutput{Name: "alice", Age: 30}},
					{`{"name":"bob","age":42}`, writeMethodOutput{Name: "bob", Age: 42}},
				} {
					req.Body = io.NopCloser(strings.NewReader(tc.body))
					req.ContentLength = int64(len(tc.body))
					req.Form, req.PostForm, req.MultipartForm = nil, nil, nil
					rec := httptest.NewRecorder()
					s.ServeHTTP(rec, req)
					if rec.Code != http.StatusOK {
						t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
					}
					var got writeMethodOutput
					if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
						t.Fatal(err)
					}
					if got != tc.want {
						t.Fatalf("response = %+v, want %+v", got, tc.want)
					}
				}
			})
		}
	}
}

func BenchmarkWriteMethodsSmallJSON(b *testing.B) {
	const body = `{"name":"alice","age":30}`
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		b.Run(method, func(b *testing.B) {
			for _, version := range []string{"v1", "v2", "v2DecodeWith"} {
				b.Run(version, func(b *testing.B) {
					s, err := writeMethodServer(version, method)
					if err != nil {
						b.Fatal(err)
					}
					req := httptest.NewRequest(method, "/resource", nil)
					req.Header.Set("Content-Type", "application/json")
					w := &writeMethodWriter{header: make(http.Header)}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						resetWriteMethodRequest(req, body, w)
						s.ServeHTTP(w, req)
						if w.status != http.StatusOK {
							b.Fatalf("%s %s: status = %d", method, version, w.status)
						}
					}
				})
			}
		})
	}
}
