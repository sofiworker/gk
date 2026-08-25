package ghttp

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// buildMultipart 构造一个 multipart 请求体,files 为 name→[]文件(内容),fields 为文本字段。
// buildMultipart builds a multipart body; files maps name→[]file contents,
// fields carries text fields.
func buildMultipart(t *testing.T, files map[string][]fileContent, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for name, val := range fields {
		if err := w.WriteField(name, val); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	for name, fs := range files {
		for _, fc := range fs {
			part, err := w.CreateFormFile(name, fc.filename)
			if err != nil {
				t.Fatalf("create form file: %v", err)
			}
			if _, err := part.Write([]byte(fc.data)); err != nil {
				t.Fatalf("write part: %v", err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

type fileContent struct {
	filename string
	data     string
}

// 文件现在归请求体:所有上传字段放进 form 请求体结构体,经 FormBody[T]() 解码。
// Files now belong to the request body: all upload fields live in a form body
// struct, decoded via FormBody[T]().

// ——— 多文件上传 []Upload ——— //

type multiUploadBody struct {
	Files []Upload `form:"files"`
}

type multiUploadResult struct {
	Count int      `json:"count"`
	Names []string `json:"names"`
	Sizes []int64  `json:"sizes"`
}

func TestUpload_MultipleFiles(t *testing.T) {
	m := New()
	if err := PostBody(m, "/multi", FormBody[multiUploadBody](), JSON[multiUploadResult](),
		func(_ context.Context, b multiUploadBody) (multiUploadResult, error) {
			res := multiUploadResult{Count: len(b.Files)}
			for _, f := range b.Files {
				res.Names = append(res.Names, f.Filename)
				res.Sizes = append(res.Sizes, f.Size)
			}
			return res, nil
		}); err != nil {
		t.Fatal(err)
	}
	body, ct := buildMultipart(t, map[string][]fileContent{
		"files": {{"a.txt", "aaa"}, {"b.txt", "bbbbb"}, {"c.txt", "c"}},
	}, nil)
	req := httptest.NewRequest(http.MethodPost, "/multi", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	got := rec.Body.String()
	if !strings.Contains(got, `"count":3`) ||
		!strings.Contains(got, `"a.txt"`) || !strings.Contains(got, `"b.txt"`) || !strings.Contains(got, `"c.txt"`) {
		t.Fatalf("unexpected body: %s", got)
	}
	// 尺寸应为 3/5/1。Sizes should be 3/5/1.
	if !strings.Contains(got, `"sizes":[3,5,1]`) {
		t.Fatalf("sizes mismatch: %s", got)
	}
}

// ——— 可选文件:缺失时保留零值,不报错 ——— //

type optionalUploadBody struct {
	Avatar Upload `form:"avatar"` // 无文件即零值。zero value when absent
	Name   string `form:"name"`
}

type optionalUploadResult struct {
	HasFile  bool   `json:"has_file"`
	Filename string `json:"filename"`
	Name     string `json:"name"`
}

func TestUpload_OptionalMissing(t *testing.T) {
	m := New()
	if err := PostBody(m, "/opt", FormBody[optionalUploadBody](), JSON[optionalUploadResult](),
		func(_ context.Context, b optionalUploadBody) (optionalUploadResult, error) {
			return optionalUploadResult{
				HasFile:  b.Avatar.Open != nil,
				Filename: b.Avatar.Filename,
				Name:     b.Name,
			}, nil
		}); err != nil {
		t.Fatal(err)
	}
	// 只带文本字段,不带文件。Only a text field, no file.
	body, ct := buildMultipart(t, nil, map[string]string{"name": "alice"})
	req := httptest.NewRequest(http.MethodPost, "/opt", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	got := rec.Body.String()
	if !strings.Contains(got, `"has_file":false`) || !strings.Contains(got, `"name":"alice"`) {
		t.Fatalf("expected has_file=false + name=alice, body=%s", got)
	}
}

func TestUpload_OptionalPresent(t *testing.T) {
	m := New()
	if err := PostBody(m, "/opt2", FormBody[optionalUploadBody](), JSON[optionalUploadResult](),
		func(_ context.Context, b optionalUploadBody) (optionalUploadResult, error) {
			return optionalUploadResult{HasFile: b.Avatar.Open != nil, Filename: b.Avatar.Filename}, nil
		}); err != nil {
		t.Fatal(err)
	}
	body, ct := buildMultipart(t, map[string][]fileContent{
		"avatar": {{"pic.png", "imgdata"}},
	}, nil)
	req := httptest.NewRequest(http.MethodPost, "/opt2", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"filename":"pic.png"`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// ——— Upload.Save 与 Upload.Bytes ——— //

func TestUpload_SaveAndBytes(t *testing.T) {
	dir := t.TempDir()
	savePath := filepath.Join(dir, "saved.bin")
	const payload = "the quick brown fox"

	m := New()
	if err := PostBody(m, "/save", FormBody[uploadBody](), JSON[uploadResult](),
		func(_ context.Context, b uploadBody) (uploadResult, error) {
			// Bytes 读取整体。Bytes reads the whole content.
			data, err := b.File.Bytes()
			if err != nil {
				return uploadResult{}, err
			}
			// Save 落盘。Save writes to disk.
			if err := b.File.Save(savePath); err != nil {
				return uploadResult{}, err
			}
			return uploadResult{Filename: b.File.Filename, Size: len(data)}, nil
		}); err != nil {
		t.Fatal(err)
	}
	body, ct := buildMultipart(t, map[string][]fileContent{
		"file": {{"doc.txt", payload}},
	}, nil)
	req := httptest.NewRequest(http.MethodPost, "/save", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	saved, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("read saved: %v", err)
	}
	if string(saved) != payload {
		t.Fatalf("saved content mismatch: got %q want %q", saved, payload)
	}
}

// ——— ContentType 字段填充 ——— //

func TestUpload_ContentTypeField(t *testing.T) {
	m := New()
	if err := PostBody(m, "/ct", FormBody[uploadBody](), JSON[map[string]string](),
		func(_ context.Context, b uploadBody) (map[string]string, error) {
			return map[string]string{"ct": b.File.ContentType}, nil
		}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	mh := make(textproto.MIMEHeader)
	mh.Set("Content-Disposition", `form-data; name="file"; filename="a.json"`)
	mh.Set("Content-Type", "application/json")
	part, err := w.CreatePart(mh)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(`{"k":1}`))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/ct", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "application/json") {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// ——— 非 multipart 请求命中可选 upload 字段:不 panic,保留零值 ——— //

func TestUpload_NonMultipartOptional(t *testing.T) {
	m := New()
	if err := PostBody(m, "/nm", FormBody[optionalUploadBody](), JSON[optionalUploadResult](),
		func(_ context.Context, b optionalUploadBody) (optionalUploadResult, error) {
			return optionalUploadResult{HasFile: b.Avatar.Open != nil, Name: b.Name}, nil
		}); err != nil {
		t.Fatal(err)
	}
	// 发送 urlencoded 而非 multipart。Send urlencoded, not multipart.
	req := httptest.NewRequest(http.MethodPost, "/nm", strings.NewReader("name=bob"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"has_file":false`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// ——— Bytes/Save 在未绑定 Upload 上返回错误而非 panic ——— //

func TestUpload_UnboundBytesSave(t *testing.T) {
	var u Upload
	if _, err := u.Bytes(); err == nil {
		t.Fatal("expected error from Bytes on unbound upload")
	}
	if err := u.Save(filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("expected error from Save on unbound upload")
	}
}

// ——— 多个不同名 upload 字段 + 混合文本字段(同一请求体) ——— //

type twoFieldUploadBody struct {
	Avatar Upload   `form:"avatar"`
	Docs   []Upload `form:"docs"`
	Tag    string   `form:"tag"`
}

type twoFieldResult struct {
	Avatar   string   `json:"avatar"`
	Tag      string   `json:"tag"`
	DocCount int      `json:"doc_count"`
	DocNames []string `json:"doc_names"`
}

func TestUpload_MultipleDistinctFields(t *testing.T) {
	m := New()
	if err := PostBody(m, "/two", FormBody[twoFieldUploadBody](), JSON[twoFieldResult](),
		func(_ context.Context, b twoFieldUploadBody) (twoFieldResult, error) {
			res := twoFieldResult{Avatar: b.Avatar.Filename, Tag: b.Tag, DocCount: len(b.Docs)}
			for _, d := range b.Docs {
				res.DocNames = append(res.DocNames, d.Filename)
			}
			return res, nil
		}); err != nil {
		t.Fatal(err)
	}
	body, ct := buildMultipart(t, map[string][]fileContent{
		"avatar": {{"me.png", "x"}},
		"docs":   {{"d1.pdf", "11"}, {"d2.pdf", "22"}},
	}, map[string]string{"tag": "profile"})
	req := httptest.NewRequest(http.MethodPost, "/two", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	got := rec.Body.String()
	if !strings.Contains(got, `"avatar":"me.png"`) || !strings.Contains(got, `"doc_count":2`) ||
		!strings.Contains(got, `"tag":"profile"`) ||
		!strings.Contains(got, `"d1.pdf"`) || !strings.Contains(got, `"d2.pdf"`) {
		t.Fatalf("unexpected body: %s", got)
	}
}

// ——— path 参数 + 含文件的表单请求体(PutParamsBody) ——— //

func TestUpload_PutParamsBodyWithFiles(t *testing.T) {
	m := New()
	if err := PutParamsBody(m, "/users/{id}/files", FormBody[multiUploadBody](), JSON[multiUploadResult](),
		func(_ context.Context, p mixedUploadPathParams, b multiUploadBody) (multiUploadResult, error) {
			res := multiUploadResult{Count: len(b.Files)}
			for _, f := range b.Files {
				res.Names = append(res.Names, f.Filename)
			}
			// 通过在名字里带上 user id 间接验证 path 参数已绑定。
			// Indirectly assert the path param bound by echoing the user id.
			res.Names = append(res.Names, "uid="+strconv.FormatInt(p.UserID, 10))
			return res, nil
		}); err != nil {
		t.Fatal(err)
	}
	body, ct := buildMultipart(t, map[string][]fileContent{
		"files": {{"a", "1"}, {"b", "2"}},
	}, nil)
	req := httptest.NewRequest(http.MethodPut, "/users/9/files", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	got := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(got, `"count":2`) || !strings.Contains(got, "uid=9") {
		t.Fatalf("code=%d body=%s", rec.Code, got)
	}
}
