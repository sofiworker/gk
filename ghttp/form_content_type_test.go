package ghttp

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 缺陷 A(M9)回归:表单契约此前 ContentType() 返回空串,而空串在严格校验里等于【放行】。
// 于是把 application/json 的请求体发到表单端点会被放行,表单解析又不认识那个
// Content-Type、根本不读请求体,最终静默解出零值结构体并回 200——调用方数据被丢弃却
// 毫无提示。修复后表单经 ContentTypes() 声明两个可接受类型,不属于该集合的一律 415。

type ctFormBody struct {
	Name  string `form:"name" json:"name"`
	Count int    `form:"count" json:"count"`
}

type ctFormOut struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// newFormServer 注册一个指定方法的表单端点,并把 handler 收到的 body 原样回写,
// 使"解出零值"与"解出真实值"在响应体上可区分。
func newFormServer(t *testing.T, method string, opts ...Option) *Server {
	t.Helper()
	s := New(opts...)
	h := func(_ context.Context, b ctFormBody) (ctFormOut, error) {
		return ctFormOut{Name: b.Name, Count: b.Count}, nil
	}
	var err error
	switch method {
	case http.MethodPost:
		err = PostBody(s, "/form", FormBody[ctFormBody](), JSON[ctFormOut](), h)
	case http.MethodPut:
		err = PutBody(s, "/form", FormBody[ctFormBody](), JSON[ctFormOut](), h)
	case http.MethodPatch:
		err = PatchBody(s, "/form", FormBody[ctFormBody](), JSON[ctFormOut](), h)
	case http.MethodDelete:
		err = DeleteBody(s, "/form", FormBody[ctFormBody](), JSON[ctFormOut](), h)
	default:
		t.Fatalf("unsupported method %q", method)
	}
	if err != nil {
		t.Fatalf("register %s: %v", method, err)
	}
	return s
}

// multipartFormBody 构造一份 multipart 请求体,返回体与其 Content-Type。
func multipartFormBody(t *testing.T, name, count string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("name", name); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("count", count); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func TestFormBody_DeclaresBothFormContentTypes(t *testing.T) {
	dec := formCodec{}
	// 单值代表取更常见的 urlencoded,供只认单值契约的调用方与 OpenAPI 使用。
	if got := dec.ContentType(); got != contentTypeFormURLEncoded {
		t.Errorf("ContentType() = %q, want %q", got, contentTypeFormURLEncoded)
	}
	// 完整集合必须两者兼有,否则严格校验会误拒一种合法表单类型。
	var multi MultiContentTypeDecoder = dec
	got := multi.ContentTypes()
	if len(got) != 2 {
		t.Fatalf("ContentTypes() = %v, want 2 entries", got)
	}
	want := map[string]bool{contentTypeFormURLEncoded: false, contentTypeMultipartForm: false}
	for _, ct := range got {
		if _, ok := want[ct]; !ok {
			t.Errorf("unexpected content type %q in %v", ct, got)
			continue
		}
		want[ct] = true
	}
	for ct, seen := range want {
		if !seen {
			t.Errorf("ContentTypes() = %v, missing %q", got, ct)
		}
	}
}

// TestFormBody_StrictContentTypeRejectsNonForm 覆盖严格模式下表单端点对各类请求
// Content-Type 的判定。非表单类型必须 415,而不能静默 200 零值。
func TestFormBody_StrictContentTypeRejectsNonForm(t *testing.T) {
	cases := []struct {
		name       string
		ct         string
		body       string
		wantStatus int
		// wantBody 非空时断言响应体,用于证明数据真的解出来了(而非零值)。
		wantBody string
	}{
		{
			name:       "json content type is rejected",
			ct:         "application/json",
			body:       `{"name":"alice","count":7}`,
			wantStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:       "xml content type is rejected",
			ct:         "application/xml",
			body:       "<b><name>alice</name></b>",
			wantStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:       "text content type is rejected",
			ct:         "text/plain",
			body:       "name=alice&count=7",
			wantStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:       "urlencoded is accepted and decoded",
			ct:         contentTypeFormURLEncoded,
			body:       "name=alice&count=7",
			wantStatus: http.StatusOK,
			wantBody:   `{"name":"alice","count":7}`,
		},
		{
			name:       "urlencoded with charset parameter is accepted",
			ct:         contentTypeFormURLEncoded + "; charset=utf-8",
			body:       "name=alice&count=7",
			wantStatus: http.StatusOK,
			wantBody:   `{"name":"alice","count":7}`,
		},
		{
			// 大小写不敏感:媒体类型比对前已归一化为小写。
			name:       "uppercase urlencoded is accepted",
			ct:         "APPLICATION/X-WWW-FORM-URLENCODED",
			body:       "name=alice&count=7",
			wantStatus: http.StatusOK,
			wantBody:   `{"name":"alice","count":7}`,
		},
		{
			// 缺省 Content-Type 仍放行(与 JSON 端点一致的既有契约:交给解码器处理)。
			name:       "missing content type still passes",
			ct:         "",
			body:       "name=alice&count=7",
			wantStatus: http.StatusOK,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newFormServer(t, http.MethodPost)
			req := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader(tc.body))
			if tc.ct == "" {
				req.Header.Del("Content-Type")
			} else {
				req.Header.Set("Content-Type", tc.ct)
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("body = %s, want it to contain %s", rec.Body.String(), tc.wantBody)
			}
		})
	}
}

func TestFormBody_MultipartAcceptedUnderStrictContentType(t *testing.T) {
	body, ct := multipartFormBody(t, "bob", "9")
	s := newFormServer(t, http.MethodPost)
	req := httptest.NewRequest(http.MethodPost, "/form", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"bob"`) {
		t.Errorf("body = %s, want name bob", rec.Body.String())
	}
}

// TestFormBody_RejectionReportsUnsupportedMediaType 断言拒绝走的是统一错误链的 415
// 哨兵(而非某个偶然同码的错误),且错误消息报出两个可接受类型以便调用方自查。
func TestFormBody_RejectionReportsUnsupportedMediaType(t *testing.T) {
	var hookStatus int
	var hookErr error
	s := New(WithErrorHook(func(_ *http.Request, status int, err error) {
		hookStatus, hookErr = status, err
	}))
	if err := PostBody(s, "/form", FormBody[ctFormBody](), JSON[ctFormOut](),
		func(_ context.Context, b ctFormBody) (ctFormOut, error) {
			return ctFormOut{Name: b.Name, Count: b.Count}, nil
		}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
	if hookStatus != http.StatusUnsupportedMediaType {
		t.Errorf("hook status = %d, want 415", hookStatus)
	}
	if !errors.Is(hookErr, ErrUnsupportedMediaType) {
		t.Fatalf("hook err = %v, want ErrUnsupportedMediaType", hookErr)
	}
	msg := hookErr.Error()
	for _, want := range []string{contentTypeFormURLEncoded, contentTypeMultipartForm, "application/json"} {
		if !strings.Contains(msg, want) {
			t.Errorf("err %q should mention %q", msg, want)
		}
	}
}

// TestFormBody_LenientModeStillPassesNonForm 确认关闭严格校验后放行行为不变:415 只是
// 严格模式的判定,不是解码器的硬约束。
func TestFormBody_LenientModeStillPassesNonForm(t *testing.T) {
	s := newFormServer(t, http.MethodPost, WithStrictContentType(false))
	req := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 in lenient mode (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestFormBody_DeclaredContentTypeCodecsUnaffected 是对照组:声明了具体单值 CT 的
// JSON/XML/text 契约行为必须与改动前一致——匹配放行、不匹配 415。
func TestFormBody_DeclaredContentTypeCodecsUnaffected(t *testing.T) {
	cases := []struct {
		name       string
		in         InputSpec[ctFormBody]
		ct         string
		body       string
		wantStatus int
	}{
		{"json accepts json", JSONBody[ctFormBody](), "application/json", `{"name":"a","count":1}`, http.StatusOK},
		{"json rejects urlencoded", JSONBody[ctFormBody](), contentTypeFormURLEncoded, "name=a", http.StatusUnsupportedMediaType},
		{"json rejects multipart", JSONBody[ctFormBody](), contentTypeMultipartForm, "x", http.StatusUnsupportedMediaType},
		{"xml accepts xml", XMLBody[ctFormBody](), "application/xml", `<ctFormBody><Name>a</Name></ctFormBody>`, http.StatusOK},
		{"xml rejects json", XMLBody[ctFormBody](), "application/json", `{"name":"a"}`, http.StatusUnsupportedMediaType},
		{"xml rejects urlencoded", XMLBody[ctFormBody](), contentTypeFormURLEncoded, "name=a", http.StatusUnsupportedMediaType},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			if err := PostBody(s, "/x", tc.in, JSON[ctFormOut](),
				func(_ context.Context, b ctFormBody) (ctFormOut, error) {
					return ctFormOut{Name: b.Name, Count: b.Count}, nil
				}); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.ct)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

// TestFormBody_DecodesBodyOnEveryMethod 覆盖 M9 后果 3:标准库只为 POST/PUT/PATCH 读
// 请求体填 PostForm,对 DELETE 等方法只解析查询串。此前 urlencoded 分支直接依赖
// PostForm,故 DeleteBody + FormBody 静默解出零值结构体。
func TestFormBody_DecodesBodyOnEveryMethod(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method+"_urlencoded", func(t *testing.T) {
			s := newFormServer(t, method)
			req := httptest.NewRequest(method, "/form", strings.NewReader("name=carol&count=11"))
			req.Header.Set("Content-Type", contentTypeFormURLEncoded)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"name":"carol"`) ||
				!strings.Contains(rec.Body.String(), `"count":11`) {
				t.Fatalf("body = %s, want decoded carol/11 (zero values mean the body was never read)",
					rec.Body.String())
			}
		})
		t.Run(method+"_multipart", func(t *testing.T) {
			body, ct := multipartFormBody(t, "dave", "13")
			s := newFormServer(t, method)
			req := httptest.NewRequest(method, "/form", body)
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"name":"dave"`) {
				t.Fatalf("body = %s, want decoded dave", rec.Body.String())
			}
		})
	}
}

// TestFormBody_DeleteMalformedBodyReports400 确认非 POST 分支的解析错误同样进统一错误
// 链(而非被吞成零值 200)。
func TestFormBody_DeleteMalformedBodyReports400(t *testing.T) {
	s := newFormServer(t, http.MethodDelete)
	req := httptest.NewRequest(http.MethodDelete, "/form", strings.NewReader("name=%zz"))
	req.Header.Set("Content-Type", contentTypeFormURLEncoded)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestFormBody_DeleteBodySurvivesEarlierParseForm 确认上游中间件对 DELETE 调用过
// ParseForm(CSRF 就会这么做)之后,请求体仍能被表单解码读到:标准库对 DELETE 只把
// PostForm 置为空值而不消费 body,故补读仍然有效。
func TestFormBody_DeleteBodySurvivesEarlierParseForm(t *testing.T) {
	s := New()
	s.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			_ = req.ParseForm()
			return next(ctx, req, resp)
		}
	})
	if err := DeleteBody(s, "/form", FormBody[ctFormBody](), JSON[ctFormOut](),
		func(_ context.Context, b ctFormBody) (ctFormOut, error) {
			return ctFormOut{Name: b.Name, Count: b.Count}, nil
		}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/form", strings.NewReader("name=erin&count=5"))
	req.Header.Set("Content-Type", contentTypeFormURLEncoded)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"erin"`) {
		t.Fatalf("body = %s, want decoded erin", rec.Body.String())
	}
}

// TestContentTypeIn 直接覆盖集合判定的边界:空请求 CT 放行、空集合不校验、
// 参数与大小写归一化。
func TestContentTypeIn(t *testing.T) {
	formSet := []string{contentTypeFormURLEncoded, contentTypeMultipartForm}
	cases := []struct {
		name string
		got  string
		want []string
		ok   bool
	}{
		{"empty request ct passes", "", formSet, true},
		{"empty want set skips check", "application/json", nil, true},
		{"first member matches", contentTypeFormURLEncoded, formSet, true},
		{"second member matches", contentTypeMultipartForm, formSet, true},
		{"parameters are ignored", contentTypeMultipartForm + "; boundary=xyz", formSet, true},
		{"case is normalized", "Application/X-WWW-Form-UrlEncoded", formSet, true},
		{"surrounding space is trimmed", "  " + contentTypeFormURLEncoded + "  ", formSet, true},
		{"non member is rejected", "application/json", formSet, false},
		{"prefix is not a match", "application/x-www-form", formSet, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := contentTypeIn(tc.got, tc.want); got != tc.ok {
				t.Errorf("contentTypeIn(%q, %v) = %v, want %v", tc.got, tc.want, got, tc.ok)
			}
		})
	}
}

// ctSingleDecoder 只实现 RequestDecoder(不实现 MultiContentTypeDecoder),用于确认
// 回退到单值语义。
type ctSingleDecoder struct{ ct string }

func (d ctSingleDecoder) ContentType() string      { return d.ct }
func (ctSingleDecoder) Decode(*Request, any) error { return nil }

// ctMultiDecoder 同时实现两者,且声明含空项与带参数项,用于确认注册期归一化。
type ctMultiDecoder struct{ cts []string }

func (ctMultiDecoder) ContentType() string        { return "application/json" }
func (ctMultiDecoder) Decode(*Request, any) error { return nil }
func (d ctMultiDecoder) ContentTypes() []string   { return d.cts }

func TestAcceptedContentTypes(t *testing.T) {
	cases := []struct {
		name string
		dec  RequestDecoder
		want []string
	}{
		{"single valued decoder", ctSingleDecoder{ct: "application/json"}, []string{"application/json"}},
		{"single valued decoder is normalized", ctSingleDecoder{ct: "Application/JSON; charset=utf-8"}, []string{"application/json"}},
		{"single valued empty declaration opts out", ctSingleDecoder{ct: ""}, nil},
		{"multi valued declaration wins over single", ctMultiDecoder{cts: []string{"text/csv", "text/tab-separated-values"}}, []string{"text/csv", "text/tab-separated-values"}},
		{"multi valued entries are normalized", ctMultiDecoder{cts: []string{"TEXT/CSV; charset=utf-8"}}, []string{"text/csv"}},
		{"empty multi declaration falls back to single", ctMultiDecoder{cts: nil}, []string{"application/json"}},
		{"blank multi entries fall back to single", ctMultiDecoder{cts: []string{"", "  "}}, []string{"application/json"}},
		{"form decoder declares both form types", formCodec{}, []string{contentTypeFormURLEncoded, contentTypeMultipartForm}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := acceptedContentTypes(tc.dec)
			if len(got) != len(tc.want) {
				t.Fatalf("acceptedContentTypes = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("acceptedContentTypes = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
