package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

type bodyPayload struct {
	Name string `json:"name"`
}

type formPayload struct {
	Name string `form:"name"`
	Age  int    `form:"age"`
}

type multipartPayload struct {
	Name   string      `form:"name"`
	Age    int         `form:"age"`
	Tags   []string    `form:"tags"`
	Avatar *FileHeader `form:"avatar"`
}

type lazyBodyFieldReq struct {
	Params
	Payload Body[bodyPayload]
}

type multiBodyReq struct {
	A Body[bodyPayload]
	B Body[bodyPayload]
}

type ptrBodyReq struct {
	Payload *Body[bodyPayload]
}

// countingReadCloser 统计底层流被读取的字节数,用于断言 body 只读一次。
// countingReadCloser counts bytes read from the underlying stream to assert
// the body is read exactly once.
type countingReadCloser struct {
	io.Reader
	reads atomic.Int64
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p)
	c.reads.Add(int64(n))
	return n, err
}

func (c *countingReadCloser) Close() error { return nil }

type viewReq struct {
	Params
}

func TestParamsRawBodySharedWithMiddleware(t *testing.T) {
	payload := `{"name":"alice"}`
	body := &countingReadCloser{Reader: strings.NewReader(payload)}

	var got struct {
		mwRaw      []byte
		handlerRaw []byte
		method     string
		ct         string
		urlPath    string
		detached   []byte
	}

	app := New(WithProduces(MIMEJSON))
	Route[viewReq, testOutput](app).
		POST("/users/{id}").
		Consumes(MIMEJSON).
		Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := RawBody(r)
				if err != nil {
					t.Errorf("middleware raw body: %v", err)
					return
				}
				got.mwRaw = append([]byte(nil), raw...)
				next.ServeHTTP(w, r)
			})
		}).
		To(func(ctx context.Context, req viewReq) (testOutput, error) {
			if req.Request() == nil {
				t.Error("request is nil")
			}
			raw, err := req.RawBody()
			if err != nil {
				return testOutput{}, err
			}
			got.handlerRaw = append([]byte(nil), raw...)
			got.method = req.Method()
			got.ct = req.ContentType()
			if req.URL() != nil {
				got.urlPath = req.URL().Path
			}
			detachedRaw, _ := req.Params.Detach().RawBody()
			got.detached = append([]byte(nil), detachedRaw...)
			return testOutput{Body: struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{ID: req.Path("id"), Name: "ok"}}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/users/42", body)
	r.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if string(got.mwRaw) != payload {
		t.Errorf("middleware raw = %q, want %q", got.mwRaw, payload)
	}
	if string(got.handlerRaw) != payload {
		t.Errorf("handler raw = %q, want %q", got.handlerRaw, payload)
	}
	if got.method != http.MethodPost {
		t.Errorf("method = %q", got.method)
	}
	if got.ct != "application/json" {
		t.Errorf("content type = %q", got.ct)
	}
	if got.urlPath != "/users/42" {
		t.Errorf("url path = %q", got.urlPath)
	}
	if got.detached != nil {
		t.Errorf("detached raw body = %q, want nil", got.detached)
	}
	if n := body.reads.Load(); n != int64(len(payload)) {
		t.Errorf("underlying stream read %d bytes, want %d", n, len(payload))
	}
}

func TestParamsExtendedViewZeroValueSafe(t *testing.T) {
	var p Params
	if p.Request() != nil {
		t.Error("zero value request should be nil")
	}
	if p.Method() != "" {
		t.Errorf("zero value method = %q", p.Method())
	}
	if p.URL() != nil {
		t.Error("zero value url should be nil")
	}
	if p.ContentType() != "" {
		t.Errorf("zero value content type = %q", p.ContentType())
	}
	raw, err := p.RawBody()
	if raw != nil || err != nil {
		t.Errorf("zero value raw body = (%v, %v), want (nil, nil)", raw, err)
	}
}

func TestBodyFieldDecodeLazyAndCached(t *testing.T) {
	payloadBytes := `{"name":"alice"}`
	body := &countingReadCloser{Reader: strings.NewReader(payloadBytes)}

	var names []string
	app := New(WithProduces(MIMEJSON))
	Route[lazyBodyFieldReq, testOutput](app).
		POST("/p").
		Consumes(MIMEJSON).
		To(func(ctx context.Context, req lazyBodyFieldReq) (testOutput, error) {
			if n := body.reads.Load(); n != 0 {
				t.Errorf("body read before Decode: %d bytes", n)
			}
			a, err := req.Payload.Decode()
			if err != nil {
				return testOutput{}, err
			}
			b, err := req.Payload.Decode()
			if err != nil {
				return testOutput{}, err
			}
			if a != b {
				t.Errorf("decoded values differ: %+v vs %+v", a, b)
			}
			names = append(names, a.Name, b.Name)
			return testOutput{Body: struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{Name: "ok"}}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/p", body)
	r.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(names) != 2 || names[0] != "alice" || names[1] != "alice" {
		t.Errorf("decoded names = %v", names)
	}
	if n := body.reads.Load(); n != int64(len(payloadBytes)) {
		t.Errorf("underlying stream read %d bytes, want %d", n, len(payloadBytes))
	}
}

func TestBodyFieldRawSharedWithMiddlewareAndDecode(t *testing.T) {
	payloadBytes := `{"name":"bob"}`
	body := &countingReadCloser{Reader: strings.NewReader(payloadBytes)}

	var got struct {
		mw, raw, name []byte
	}
	app := New(WithProduces(MIMEJSON))
	Route[lazyBodyFieldReq, testOutput](app).
		POST("/p").
		Consumes(MIMEJSON).
		Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := RawBody(r)
				if err != nil {
					t.Errorf("middleware raw body: %v", err)
					return
				}
				got.mw = append([]byte(nil), raw...)
				next.ServeHTTP(w, r)
			})
		}).
		To(func(ctx context.Context, req lazyBodyFieldReq) (testOutput, error) {
			raw, err := req.Payload.Raw()
			if err != nil {
				return testOutput{}, err
			}
			got.raw = append([]byte(nil), raw...)
			p, err := req.Payload.Decode()
			if err != nil {
				return testOutput{}, err
			}
			got.name = append(got.name, p.Name...)
			return testOutput{Body: struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{Name: p.Name}}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/p", body)
	r.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if string(got.mw) != payloadBytes || string(got.raw) != payloadBytes {
		t.Errorf("mw = %q, raw = %q, want %q", got.mw, got.raw, payloadBytes)
	}
	if string(got.name) != "bob" {
		t.Errorf("decoded name = %q", got.name)
	}
	if n := body.reads.Load(); n != int64(len(payloadBytes)) {
		t.Errorf("underlying stream read %d bytes, want %d", n, len(payloadBytes))
	}
}

func TestBodyTopLevelShorthand(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Body[bodyPayload], testOutput](app).
		POST("/top").
		Consumes(MIMEJSON).
		To(func(ctx context.Context, b Body[bodyPayload]) (testOutput, error) {
			p, err := b.Decode()
			if err != nil {
				return testOutput{}, err
			}
			if p.Name != "alice" {
				t.Errorf("name = %q", p.Name)
			}
			return testOutput{Body: struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{Name: p.Name}}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/top", strings.NewReader(`{"name":"alice"}`))
	r.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestBodyZeroValueErrors(t *testing.T) {
	var b Body[bodyPayload]
	if _, err := b.Raw(); !errors.Is(err, ErrInvalidBody) {
		t.Errorf("Raw error = %v, want ErrInvalidBody", err)
	}
	if _, err := b.Decode(); !errors.Is(err, ErrInvalidBody) {
		t.Errorf("Decode error = %v, want ErrInvalidBody", err)
	}
	if ct := b.ContentType(); ct != "" {
		t.Errorf("zero value content type = %q", ct)
	}
}

func TestBodyDecodeXMLContentType(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Body[bodyPayload], testOutput](app).
		POST("/xml").
		Consumes(MIMEXML).
		To(func(ctx context.Context, b Body[bodyPayload]) (testOutput, error) {
			p, err := b.Decode()
			if err != nil {
				return testOutput{}, err
			}
			if p.Name != "alice" {
				t.Errorf("name = %q", p.Name)
			}
			return testOutput{Body: struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{Name: p.Name}}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/xml", strings.NewReader(`<bodyPayload><Name>alice</Name></bodyPayload>`))
	r.Header.Set("Content-Type", MIMEXML)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestBodyDecodeMissingContentTypeFallsBackToJSON(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Body[bodyPayload], testOutput](app).
		POST("/noct").
		To(func(ctx context.Context, b Body[bodyPayload]) (testOutput, error) {
			p, err := b.Decode()
			if err != nil {
				return testOutput{}, err
			}
			if p.Name != "alice" {
				t.Errorf("name = %q", p.Name)
			}
			return testOutput{Body: struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{Name: p.Name}}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/noct", strings.NewReader(`{"name":"alice"}`))
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestBodyDecodeUnsupportedContentType(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Body[bodyPayload], testOutput](app).
		POST("/bin").
		To(func(ctx context.Context, b Body[bodyPayload]) (testOutput, error) {
			_, err := b.Decode()
			if !errors.Is(err, ErrInvalidBody) {
				t.Errorf("error = %v, want ErrInvalidBody", err)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/bin", strings.NewReader("data"))
	r.Header.Set("Content-Type", "application/octet-stream")
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestBodyDecodeURLEncodedForm(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Body[formPayload], testOutput](app).
		POST("/form").
		Consumes(MIMEPOSTForm).
		To(func(ctx context.Context, b Body[formPayload]) (testOutput, error) {
			p, err := b.Decode()
			if err != nil {
				return testOutput{}, err
			}
			if p.Name != "alice" || p.Age != 7 {
				t.Errorf("payload = %+v", p)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader("name=alice&age=7"))
	r.Header.Set("Content-Type", MIMEPOSTForm)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestBodyDecodeMultipartForm(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("avatar", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("file-bytes")); err != nil {
		t.Fatal(err)
	}
	_ = mw.WriteField("name", "alice")
	_ = mw.WriteField("age", "7")
	_ = mw.WriteField("tags", "a")
	_ = mw.WriteField("tags", "b")
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	app := New(WithProduces(MIMEJSON))
	Route[Body[multipartPayload], testOutput](app).
		POST("/mp").
		Consumes(MIMEMultipartPOSTForm).
		To(func(ctx context.Context, b Body[multipartPayload]) (testOutput, error) {
			p, err := b.Decode()
			if err != nil {
				return testOutput{}, err
			}
			if p.Name != "alice" || p.Age != 7 {
				t.Errorf("payload = %+v", p)
			}
			if len(p.Tags) != 2 || p.Tags[0] != "a" || p.Tags[1] != "b" {
				t.Errorf("tags = %v", p.Tags)
			}
			if p.Avatar == nil || p.Avatar.Filename != "a.txt" {
				t.Errorf("avatar = %+v", p.Avatar)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mp", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestMiddlewarePostFormSharedWithHandlerDecode(t *testing.T) {
	bodyBytes := "name=alice&age=7"
	body := &countingReadCloser{Reader: strings.NewReader(bodyBytes)}

	var got struct {
		mwName string
		name   string
	}
	app := New(WithProduces(MIMEJSON))
	Route[Body[formPayload], testOutput](app).
		POST("/form").
		Consumes(MIMEPOSTForm).
		Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				values, err := PostFormValues(r)
				if err != nil {
					t.Errorf("middleware post form: %v", err)
					return
				}
				got.mwName = values.Get("name")
				next.ServeHTTP(w, r)
			})
		}).
		To(func(ctx context.Context, b Body[formPayload]) (testOutput, error) {
			p, err := b.Decode()
			if err != nil {
				return testOutput{}, err
			}
			got.name = p.Name
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/form", body)
	r.Header.Set("Content-Type", MIMEPOSTForm)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got.mwName != "alice" || got.name != "alice" {
		t.Errorf("mw name = %q, handler name = %q", got.mwName, got.name)
	}
	if n := body.reads.Load(); n != int64(len(bodyBytes)) {
		t.Errorf("underlying stream read %d bytes, want %d", n, len(bodyBytes))
	}
}

func TestMultipartSharedBetweenMiddlewareAndHandler(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("name", "alice")
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	body := &countingReadCloser{Reader: bytes.NewReader(buf.Bytes())}

	var got struct {
		mwName string
		name   string
	}
	app := New(WithProduces(MIMEJSON))
	Route[Body[multipartPayload], testOutput](app).
		POST("/mp").
		Consumes(MIMEMultipartPOSTForm).
		Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				form, err := MultipartForm(r, defaultMaxMemory)
				if err != nil {
					t.Errorf("middleware multipart: %v", err)
					return
				}
				got.mwName = form.Value["name"][0]
				next.ServeHTTP(w, r)
			})
		}).
		To(func(ctx context.Context, b Body[multipartPayload]) (testOutput, error) {
			p, err := b.Decode()
			if err != nil {
				return testOutput{}, err
			}
			got.name = p.Name
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mp", body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got.mwName != "alice" || got.name != "alice" {
		t.Errorf("mw name = %q, handler name = %q", got.mwName, got.name)
	}
	if n := body.reads.Load(); n != int64(len(buf.Bytes())) {
		t.Errorf("underlying stream read %d bytes, want %d", n, len(buf.Bytes()))
	}
}

func TestURLEncodedFormDecodesAfterRawDrain(t *testing.T) {
	bodyBytes := "name=alice&age=7"
	body := &countingReadCloser{Reader: strings.NewReader(bodyBytes)}

	app := New(WithProduces(MIMEJSON))
	Route[Body[formPayload], testOutput](app).
		POST("/form").
		Consumes(MIMEPOSTForm).
		Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := RawBody(r); err != nil {
					t.Errorf("middleware raw body: %v", err)
					return
				}
				next.ServeHTTP(w, r)
			})
		}).
		To(func(ctx context.Context, b Body[formPayload]) (testOutput, error) {
			p, err := b.Decode()
			if err != nil {
				return testOutput{}, err
			}
			if p.Name != "alice" || p.Age != 7 {
				t.Errorf("payload = %+v", p)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/form", body)
	r.Header.Set("Content-Type", MIMEPOSTForm)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if n := body.reads.Load(); n != int64(len(bodyBytes)) {
		t.Errorf("underlying stream read %d bytes, want %d", n, len(bodyBytes))
	}
}

func TestParamsFormAccessors(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[viewReq, testOutput](app).
		POST("/form").
		Consumes(MIMEPOSTForm).
		To(func(ctx context.Context, req viewReq) (testOutput, error) {
			if got := req.PostForm("name"); got != "alice" {
				t.Errorf("PostForm = %q", got)
			}
			if got := req.Form("q"); got != "1" {
				t.Errorf("merged Form q = %q", got)
			}
			if got := req.Form("name"); got != "alice" {
				t.Errorf("merged Form name = %q", got)
			}
			values, err := req.PostFormValues()
			if err != nil || values.Get("name") != "alice" {
				t.Errorf("PostFormValues = %v, %v", values, err)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/form?q=1", strings.NewReader("name=alice"))
	r.Header.Set("Content-Type", MIMEPOSTForm)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestParamsFormMalformedReturnsError(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[viewReq, testOutput](app).
		POST("/form").
		Consumes(MIMEPOSTForm).
		To(func(ctx context.Context, req viewReq) (testOutput, error) {
			if _, err := req.PostFormValues(); err == nil {
				t.Error("malformed form should return an error")
			}
			if got := req.PostForm("name"); got != "" {
				t.Errorf("silent PostForm = %q on malformed body", got)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader("name=%zz"))
	r.Header.Set("Content-Type", MIMEPOSTForm)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestFormBodyRespectsMaxBodyBytes(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Body[formPayload], testOutput](app).
		POST("/form").
		MaxBodyBytes(8).
		To(func(ctx context.Context, b Body[formPayload]) (testOutput, error) {
			_, err := b.Decode()
			var maxErr *http.MaxBytesError
			if !errors.As(err, &maxErr) {
				t.Errorf("error = %v, want *http.MaxBytesError", err)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader("name=alice&age=7"))
	r.Header.Set("Content-Type", MIMEPOSTForm)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestMultipartAfterRawDrainRejected(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("name", "alice")
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	app := New(WithProduces(MIMEJSON))
	Route[Body[multipartPayload], testOutput](app).
		POST("/mp").
		Consumes(MIMEMultipartPOSTForm).
		Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := RawBody(r); err != nil {
					t.Errorf("middleware raw body: %v", err)
					return
				}
				next.ServeHTTP(w, r)
			})
		}).
		To(func(ctx context.Context, b Body[multipartPayload]) (testOutput, error) {
			_, err := b.Decode()
			if !errors.Is(err, ErrInvalidBody) {
				t.Errorf("error = %v, want ErrInvalidBody", err)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mp", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestMultipartRespectsMaxBodyBytes(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("name", "alice-with-a-long-name")
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	app := New(WithProduces(MIMEJSON))
	Route[Body[multipartPayload], testOutput](app).
		POST("/mp").
		MaxBodyBytes(32).
		To(func(ctx context.Context, b Body[multipartPayload]) (testOutput, error) {
			_, err := b.Decode()
			var maxErr *http.MaxBytesError
			if !errors.As(err, &maxErr) {
				t.Errorf("error = %v, want *http.MaxBytesError", err)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mp", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestBodyDecodeWithBodyDecoderOverride(t *testing.T) {
	var (
		called bool
		gotCT  string
	)
	app := New(
		WithProduces(MIMEJSON),
		WithBodyDecoder(func(r io.Reader, ct string, target interface{}) error {
			called = true
			gotCT = ct
			data, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			return json.Unmarshal(data, target)
		}),
	)
	Route[Body[bodyPayload], testOutput](app).
		POST("/custom").
		To(func(ctx context.Context, b Body[bodyPayload]) (testOutput, error) {
			p, err := b.Decode()
			if err != nil {
				return testOutput{}, err
			}
			if p.Name != "alice" {
				t.Errorf("name = %q", p.Name)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/custom", strings.NewReader(`{"name":"alice"}`))
	r.Header.Set("Content-Type", "application/x-custom+json")
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Error("custom body decoder was not called")
	}
	if gotCT != "application/x-custom+json" {
		t.Errorf("content type passed to decoder = %q", gotCT)
	}
}

func TestBodyDecodeMalformedJSONWrapsErrInvalidBody(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Body[bodyPayload], testOutput](app).
		POST("/bad").
		To(func(ctx context.Context, b Body[bodyPayload]) (testOutput, error) {
			_, err := b.Decode()
			if !errors.Is(err, ErrInvalidBody) {
				t.Errorf("error = %v, want ErrInvalidBody", err)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/bad", strings.NewReader(`{"name":`))
	r.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCompileJSONPathExtractAndUnmarshal(t *testing.T) {
	data := []byte(`{"items":[{"title":"t1","n":1},{"title":"t2","n":2}]}`)

	path, err := CompileJSONPath("$.items[0].title")
	if err != nil {
		t.Fatal(err)
	}
	vals, err := path.Extract(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(vals) != 1 || string(vals[0]) != `"t1"` {
		t.Fatalf("extract = %q", vals)
	}

	var title string
	if err := path.Unmarshal(data, &title); err != nil {
		t.Fatal(err)
	}
	if title != "t1" {
		t.Errorf("title = %q", title)
	}

	cached, err := CompileJSONPath("$.items[0].title")
	if err != nil {
		t.Fatal(err)
	}
	if cached != path {
		t.Error("CompileJSONPath did not reuse the cached compiled path")
	}

	if _, err := CompileJSONPath("$[bad"); err == nil {
		t.Error("invalid JSON path compiled without error")
	}
}

func TestCompileJSONPathNilAndInvalidData(t *testing.T) {
	var nilPath *JSONPath
	if _, err := nilPath.Extract([]byte(`{}`)); err == nil {
		t.Error("nil path extract should fail")
	}
	path, err := CompileJSONPath("$.a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := path.Extract([]byte(`{bad`)); err == nil {
		t.Error("extract on invalid JSON should fail")
	}
}

func TestRouteRejectsMultipleLazyBodyFields(t *testing.T) {
	app := New()
	assertRoutePanic(t, ErrMultipleBodyFields, func() {
		Route[multiBodyReq, testOutput](app).POST("/m").To(func(ctx context.Context, req multiBodyReq) (testOutput, error) {
			return testOutput{}, nil
		})
	})
}

func TestRouteRejectsPointerBodyField(t *testing.T) {
	app := New()
	assertRoutePanic(t, ErrBodyFieldMustBeValue, func() {
		Route[ptrBodyReq, testOutput](app).POST("/p").To(func(ctx context.Context, req ptrBodyReq) (testOutput, error) {
			return testOutput{}, nil
		})
	})
}

func TestBodyTopLevelConsumesRejectsOtherContentType(t *testing.T) {
	called := false
	app := New(WithProduces(MIMEJSON))
	Route[Body[bodyPayload], testOutput](app).
		POST("/c").
		Consumes(MIMEJSON).
		To(func(ctx context.Context, b Body[bodyPayload]) (testOutput, error) {
			called = true
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/c", strings.NewReader(`{"name":"alice"}`))
	r.Header.Set("Content-Type", MIMEPlain)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
	if called {
		t.Error("handler should not run for unsupported media type")
	}
}

func TestExtractBodySchemaUnwrapsLazyBody(t *testing.T) {
	fieldSchema := extractBodySchema(reflect.TypeOf(lazyBodyFieldReq{}))
	fieldProps := bodySchemaProps(t, fieldSchema)
	if fieldProps["name"] == nil {
		t.Fatalf("field-level body schema = %#v", fieldSchema)
	}

	topSchema := extractBodySchema(reflect.TypeOf(Body[bodyPayload]{}))
	topProps := bodySchemaProps(t, topSchema)
	if topProps["name"] == nil {
		t.Fatalf("top-level body schema = %#v", topSchema)
	}
}

func bodySchemaProps(t *testing.T, schema interface{}) map[string]interface{} {
	t.Helper()
	if schema == nil {
		t.Fatal("body schema is nil")
	}
	m, ok := schema.(map[string]interface{})
	if !ok {
		t.Fatalf("body schema = %#v", schema)
	}
	props, _ := m["properties"].(map[string]interface{})
	return props
}

func TestBodyDecodeEnforcesMaxBodyBytes(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Body[bodyPayload], testOutput](app).
		POST("/limited").
		MaxBodyBytes(8).
		To(func(ctx context.Context, b Body[bodyPayload]) (testOutput, error) {
			_, err := b.Decode()
			var maxErr *http.MaxBytesError
			if !errors.As(err, &maxErr) {
				t.Errorf("error = %v, want *http.MaxBytesError", err)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/limited", strings.NewReader(`{"name":"alice-with-a-long-name"}`))
	r.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestBodyNeverReadDoesNotTriggerLimit(t *testing.T) {
	body := &countingReadCloser{Reader: strings.NewReader(`{"name":"this is way over the limit"}`)}
	app := New(WithProduces(MIMEJSON))
	Route[Body[bodyPayload], testOutput](app).
		POST("/skip").
		MaxBodyBytes(4).
		To(func(ctx context.Context, b Body[bodyPayload]) (testOutput, error) {
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/skip", body)
	r.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if n := body.reads.Load(); n != 0 {
		t.Errorf("body stream was read %d bytes before any access", n)
	}
}

func TestBodyDecodeJSONIgnoresContentType(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Body[bodyPayload], testOutput](app).
		POST("/p").
		To(func(ctx context.Context, b Body[bodyPayload]) (testOutput, error) {
			p, err := b.DecodeJSON()
			if err != nil {
				return testOutput{}, err
			}
			if p.Name != "alice" {
				t.Errorf("name = %q", p.Name)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/p", strings.NewReader(`{"name":"alice"}`))
	r.Header.Set("Content-Type", MIMEXML)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestBodyDecodeXMLIgnoresContentType(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Body[bodyPayload], testOutput](app).
		POST("/p").
		To(func(ctx context.Context, b Body[bodyPayload]) (testOutput, error) {
			p, err := b.DecodeXML()
			if err != nil {
				return testOutput{}, err
			}
			if p.Name != "alice" {
				t.Errorf("name = %q", p.Name)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/p", strings.NewReader(`<bodyPayload><Name>alice</Name></bodyPayload>`))
	r.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestBodyDecodeFormIgnoresContentType(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Body[formPayload], testOutput](app).
		POST("/p").
		To(func(ctx context.Context, b Body[formPayload]) (testOutput, error) {
			p, err := b.DecodeForm()
			if err != nil {
				return testOutput{}, err
			}
			if p.Name != "alice" || p.Age != 7 {
				t.Errorf("payload = %+v", p)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/p", strings.NewReader("name=alice&age=7"))
	r.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestBodyDecodeSharesCacheAcrossKinds(t *testing.T) {
	// 首次 DecodeJSON 决定格式与结果;后续 Decode() 命中缓存,不会按
	// Content-Type(XML)重新解码而失败。
	// the first DecodeJSON fixes the format and result; a later Decode() hits
	// the cache instead of re-decoding by Content-Type (XML) and failing.
	app := New(WithProduces(MIMEJSON))
	Route[Body[bodyPayload], testOutput](app).
		POST("/p").
		To(func(ctx context.Context, b Body[bodyPayload]) (testOutput, error) {
			a, err := b.DecodeJSON()
			if err != nil {
				return testOutput{}, err
			}
			c, err := b.Decode()
			if err != nil {
				return testOutput{}, err
			}
			if a != c || a.Name != "alice" {
				t.Errorf("DecodeJSON = %+v, Decode = %+v", a, c)
			}
			return testOutput{}, nil
		})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/p", strings.NewReader(`{"name":"alice"}`))
	r.Header.Set("Content-Type", MIMEXML)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
