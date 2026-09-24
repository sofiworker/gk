package webbench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/labstack/echo/v4"
	root "github.com/sofiworker/gk/ghttp"
	v2 "github.com/sofiworker/gk/ghttp/v2"
)

const matrixPattern = "/orgs/{org}/teams/{team}/users/{user}/repos/{repo}/files/{file}"
const matrixColon = "/orgs/:org/teams/:team/users/:user/repos/:repo/files/:file"
const matrixMemory = 32 << 20
const matrixLimit = 64 << 20

var matrixVersions = []string{"v1", "v2", "DecodeWith", "Gin", "Echo", "ServeMux"}

type matrixParams struct {
	Org  string   `path:"org"`
	Team string   `path:"team"`
	User string   `path:"user"`
	Repo string   `path:"repo"`
	File string   `path:"file"`
	A    string   `query:"a"`
	B    string   `query:"b"`
	C    string   `query:"c"`
	Page int      `query:"page"`
	Tags []string `query:"tag"`
}
type matrixItem struct {
	Name   string `json:"name"`
	Values []int  `json:"values"`
}
type matrixBody struct {
	Name    string       `json:"name" form:"name"`
	Age     int          `json:"age" form:"age"`
	Payload string       `json:"payload" form:"payload"`
	Items   []matrixItem `json:"items"`
}
type matrixResult struct {
	Digest  string `json:"digest"`
	Bytes   int    `json:"bytes"`
	Payload string `json:"payload,omitempty"`
}
type matrixUpload struct {
	Title string                `form:"title"`
	File  *multipart.FileHeader `form:"file"`
}
type matrixUploadV1 struct {
	Title string      `form:"title"`
	File  root.Upload `form:"file"`
}
type matrixCase struct {
	name, method, target, ct, kind string
	body                           []byte
	want                           matrixResult
}

func matrixDigest(v any) matrixResult {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(data)
	return matrixResult{Digest: hex.EncodeToString(sum[:]), Bytes: len(data)}
}
func matrixBodyResult(in matrixBody, largeOutput bool) matrixResult {
	out := matrixDigest(in)
	if largeOutput {
		out.Payload = in.Payload
	}
	return out
}
func matrixFileResult(title string, header *multipart.FileHeader) (matrixResult, error) {
	if header == nil {
		return matrixResult{}, errors.New("missing file")
	}
	f, err := header.Open()
	if err != nil {
		return matrixResult{}, err
	}
	defer f.Close()
	hash := sha256.New()
	_, _ = hash.Write([]byte(title))
	n, err := io.Copy(hash, f)
	return matrixResult{Digest: hex.EncodeToString(hash.Sum(nil)), Bytes: int(n)}, err
}
func matrixCases(t testing.TB) []matrixCase {
	t.Helper()
	var cases []matrixCase
	for _, spec := range []struct {
		name                       string
		pathSize, querySize, extra int
	}{
		{"Params_Short", 8, 8, 0},
		{"Params_BothLong_1K", 1024, 1024, 128},
		{"Params_BothLong_4K", 4096, 4096, 128},
		{"Params_PathLong", 4096, 8, 0},
		{"Params_QueryLong", 8, 4096, 128},
		{"Params_24Keys", 8, 8, 18},
		{"Params_25Keys", 8, 8, 19},
	} {
		size := spec.pathSize
		p := matrixParams{Org: strings.Repeat("o", size), Team: strings.Repeat("t", size), User: strings.Repeat("u", size), Repo: strings.Repeat("r", size), File: strings.Repeat("f", size), A: strings.Repeat("alpha", spec.querySize), B: strings.Repeat("beta", spec.querySize), C: strings.Repeat("gamma", spec.querySize), Page: 17, Tags: []string{"first", "second"}}
		q := url.Values{"a": {p.A}, "b": {p.B}, "c": {p.C}, "page": {"17"}, "tag": p.Tags}
		if spec.extra > 0 {
			for i := 0; i < spec.extra; i++ {
				q.Set(fmt.Sprintf("unused%03d", i), strings.Repeat("z", 64))
			}
		}
		path := fmt.Sprintf("/orgs/%s/teams/%s/users/%s/repos/%s/files/%s", p.Org, p.Team, p.User, p.Repo, p.File)
		cases = append(cases, matrixCase{name: spec.name, method: "GET", target: path + "?" + q.Encode(), kind: "params", want: matrixDigest(p)})
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		in := matrixBody{Name: "alice", Age: 30, Payload: "hello", Items: []matrixItem{{Name: "nested", Values: []int{1, 2, 3}}}}
		body, _ := json.Marshal(in)
		cases = append(cases, matrixCase{name: method + "_JSON", method: method, target: "/objects/7", ct: "application/json", kind: "json", body: body, want: matrixBodyResult(in, false)})
		in = matrixBody{Name: "alice 世界", Age: 30, Payload: "a+b & c"}
		body = []byte(url.Values{"name": {in.Name}, "age": {"30"}, "payload": {in.Payload}}.Encode())
		cases = append(cases, matrixCase{name: method + "_Form", method: method, target: "/objects/7", ct: "application/x-www-form-urlencoded", kind: "form", body: body, want: matrixBodyResult(in, false)})
	}
	for _, size := range []int{64 << 10, 1 << 20} {
		in := matrixBody{Name: "large", Age: 42, Payload: strings.Repeat("x", size)}
		for i := 0; i < 1024; i++ {
			in.Items = append(in.Items, matrixItem{Name: fmt.Sprint(i), Values: []int{i, i + 1, i + 2}})
		}
		body, _ := json.Marshal(in)
		cases = append(cases, matrixCase{name: fmt.Sprintf("POST_JSON_%dKiB", size>>10), method: "POST", target: "/objects/7", ct: "application/json", kind: "json", body: body, want: matrixBodyResult(in, false)})
		if size == 1<<20 {
			cases = append(cases, matrixCase{name: "POST_JSON_1024KiB_Echo", method: "POST", target: "/objects/7", ct: "application/json", kind: "echojson", body: body, want: matrixBodyResult(in, true)})
		}
	}
	for _, size := range []int{32 << 10, 1 << 20, 40 << 20} {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		if err := w.SetBoundary("gk-benchmark-boundary"); err != nil {
			t.Fatal(err)
		}
		if err := w.WriteField("title", "sample"); err != nil {
			t.Fatal(err)
		}
		file, err := w.CreateFormFile("file", "payload.bin")
		if err != nil {
			t.Fatal(err)
		}
		content := bytes.Repeat([]byte("x"), size)
		if _, err = file.Write(content); err != nil {
			t.Fatal(err)
		}
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
		sum := sha256.New()
		_, _ = sum.Write([]byte("sample"))
		_, _ = sum.Write(content)
		cases = append(cases, matrixCase{name: fmt.Sprintf("POST_File_%dKiB", size>>10), method: "POST", target: "/objects/7", ct: w.FormDataContentType(), kind: "file", body: buf.Bytes(), want: matrixResult{Digest: hex.EncodeToString(sum.Sum(nil)), Bytes: size}})
	}
	return cases
}

// 手写适配器共用业务逻辑；decoder 差异保留并在报告中说明。
// Handwritten adapters share business logic; decoder differences remain explicit in the report.
func matrixReadParams(param func(string) string, first func(string) (string, bool), all func(string) []string) (matrixParams, error) {
	p := matrixParams{Org: param("org"), Team: param("team"), User: param("user"), Repo: param("repo"), File: param("file"), Tags: all("tag")}
	p.A, _ = first("a")
	p.B, _ = first("b")
	p.C, _ = first("c")
	if raw, ok := first("page"); ok {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return p, err
		}
		p.Page = n
	}
	return p, nil
}
func matrixDecodeBody(req *http.Request, kind string) (matrixBody, error) {
	var in matrixBody
	if kind == "form" {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return in, err
		}
		values, err := url.ParseQuery(string(raw))
		if err != nil {
			return in, err
		}
		in.Name = values.Get("name")
		in.Payload = values.Get("payload")
		if raw, ok := values["age"]; ok && len(raw) > 0 {
			in.Age, err = strconv.Atoi(raw[0])
		}
		return in, err
	}
	d := json.NewDecoder(req.Body)
	if err := d.Decode(&in); err != nil {
		return in, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return in, err
	}
	return in, nil
}
func matrixStandardRequest(req *http.Request, param func(string) string, c matrixCase) (matrixResult, error) {
	switch c.kind {
	case "params":
		q := req.URL.Query()
		first := func(k string) (string, bool) {
			v := q[k]
			if len(v) == 0 {
				return "", false
			}
			return v[0], true
		}
		in, err := matrixReadParams(param, first, func(k string) []string { return q[k] })
		return matrixDigest(in), err
	case "file":
		if err := req.ParseMultipartForm(matrixMemory); err != nil {
			return matrixResult{}, err
		}
		files := req.MultipartForm.File["file"]
		if len(files) != 1 {
			return matrixResult{}, errors.New("expected one file")
		}
		return matrixFileResult(req.FormValue("title"), files[0])
	default:
		in, err := matrixDecodeBody(req, c.kind)
		return matrixBodyResult(in, c.kind == "echojson"), err
	}
}
func matrixServer(t testing.TB, version string, c matrixCase) http.Handler {
	t.Helper()
	pattern := "/objects/{id}"
	colon := "/objects/:id"
	if c.kind == "params" {
		pattern = matrixPattern
		colon = matrixColon
	}
	var server http.Handler
	switch version {
	case "Gin":
		gin.SetMode(gin.ReleaseMode)
		g := gin.New()
		g.Handle(c.method, colon, func(ctx *gin.Context) {
			out, err := matrixStandardRequest(ctx.Request, ctx.Param, c)
			if err != nil {
				ctx.Status(400)
				return
			}
			ctx.Header("Content-Type", "application/json")
			if err = json.NewEncoder(ctx.Writer).Encode(out); err != nil {
				panic(err)
			}
		})
		server = g
	case "Echo":
		e := echo.New()
		e.HideBanner = true
		e.Add(c.method, colon, func(ctx echo.Context) error {
			out, err := matrixStandardRequest(ctx.Request(), ctx.Param, c)
			if err != nil {
				return echo.NewHTTPError(400, err.Error())
			}
			ctx.Response().Header().Set("Content-Type", "application/json")
			return json.NewEncoder(ctx.Response()).Encode(out)
		})
		server = e
	case "ServeMux":
		mux := http.NewServeMux()
		mux.HandleFunc(c.method+" "+pattern, func(w http.ResponseWriter, r *http.Request) {
			out, err := matrixStandardRequest(r, r.PathValue, c)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if err = json.NewEncoder(w).Encode(out); err != nil {
				panic(err)
			}
		})
		server = mux
	default:
		s := root.New()
		var err error
		if c.kind == "params" {
			h := func(_ context.Context, in matrixParams) (matrixResult, error) { return matrixDigest(in), nil }
			switch version {
			case "v1":
				err = root.GetParams(s, pattern, root.JSON[matrixResult](), h)
			case "v2":
				err = v2.Get(pattern, h).Mount(s)
			case "DecodeWith":
				err = v2.Get(pattern, h, v2.WithInput(v2.DecodeWith(func(_ context.Context, r *v2.Request) (matrixParams, error) {
					return matrixReadParams(r.Params.Get, r.QueryFirst, r.QueryValues)
				}))).Mount(s)
			}
		} else if c.kind == "file" {
			if version == "v1" {
				err = root.PostBody(s, pattern, root.FormBody[matrixUploadV1](), root.JSON[matrixResult](), func(_ context.Context, in matrixUploadV1) (matrixResult, error) {
					return matrixFileResult(in.Title, in.File.Header)
				})
			} else {
				h := func(_ context.Context, in matrixUpload) (matrixResult, error) {
					return matrixFileResult(in.Title, in.File)
				}
				input := v2.MultipartInput[matrixUpload](v2.MultipartLimits{MaxBytes: matrixLimit, MemoryBytes: matrixMemory})
				if version == "DecodeWith" {
					input = v2.DecodeWith(func(_ context.Context, r *v2.Request) (matrixUpload, error) {
						if err := r.ParseMultipartForm(matrixMemory); err != nil {
							return matrixUpload{}, err
						}
						files := r.MultipartForm.File["file"]
						if len(files) != 1 {
							return matrixUpload{}, errors.New("expected file")
						}
						return matrixUpload{Title: r.FormValue("title"), File: files[0]}, nil
					})
				}
				err = v2.Post(pattern, h, v2.WithInput(input), v2.WithBodyLimit(matrixLimit)).Mount(s)
			}
		} else {
			h := func(_ context.Context, in matrixBody) (matrixResult, error) {
				return matrixBodyResult(in, c.kind == "echojson"), nil
			}
			if version == "v1" {
				var in root.InputSpec[matrixBody] = root.JSONBody[matrixBody]()
				if c.kind == "form" {
					in = root.FormBody[matrixBody]()
				}
				switch c.method {
				case "POST":
					err = root.PostBody(s, pattern, in, root.JSON[matrixResult](), h)
				case "PUT":
					err = root.PutBody(s, pattern, in, root.JSON[matrixResult](), h)
				case "PATCH":
					err = root.PatchBody(s, pattern, in, root.JSON[matrixResult](), h)
				case "DELETE":
					err = root.DeleteBody(s, pattern, in, root.JSON[matrixResult](), h)
				}
			} else {
				in := v2.JSONInput[matrixBody]()
				if c.kind == "form" {
					in = v2.FormInput[matrixBody]()
				}
				if version == "DecodeWith" {
					in = v2.DecodeWith(func(_ context.Context, r *v2.Request) (matrixBody, error) { return matrixDecodeBody(r.Request, c.kind) })
				}
				err = v2.Method(c.method, pattern, h, v2.WithInput(in), v2.WithBodyLimit(matrixLimit)).Mount(s)
			}
		}
		if err != nil {
			t.Fatalf("%s/%s registration: %v", c.name, version, err)
		}
		server = s
	}
	// 统一请求上限和临时文件清理；直接 ServeHTTP 不包含真实 Server 的清理。
	// Unify body limits and temporary file cleanup; direct ServeHTTP lacks real Server cleanup.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, matrixLimit)
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()
		server.ServeHTTP(w, r)
	})
}
func matrixVerify(t testing.TB, h http.Handler, c matrixCase) {
	t.Helper()
	req := httptest.NewRequest(c.method, c.target, bytes.NewReader(c.body))
	if c.ct != "" {
		req.Header.Set("Content-Type", c.ct)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var got matrixResult
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, c.want) {
		t.Fatalf("response mismatch: bytes %d/%d digest %s/%s payload length %d/%d", got.Bytes, c.want.Bytes, got.Digest, c.want.Digest, len(got.Payload), len(c.want.Payload))
	}
}
func TestFacadeMatrixContracts(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	for _, c := range matrixCases(t) {
		for _, v := range matrixVersions {
			t.Run(c.name+"/"+v, func(t *testing.T) {
				matrixVerify(t, matrixServer(t, v, c), c)
				files, err := os.ReadDir(temp)
				if err != nil {
					t.Fatal(err)
				}
				if len(files) != 0 {
					t.Fatal("multipart temporary files leaked")
				}
			})
		}
	}
}

type matrixWriter struct {
	header        http.Header
	status, bytes int
}

func (w *matrixWriter) Header() http.Header { return w.header }
func (w *matrixWriter) WriteHeader(n int) {
	if w.status == 0 {
		w.status = n
	}
}
func (w *matrixWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	w.bytes += len(p)
	return len(p), nil
}
func BenchmarkFacadeMatrix(b *testing.B) {
	for _, c := range matrixCases(b) {
		b.Run(c.name, func(b *testing.B) {
			for _, v := range matrixVersions {
				b.Run(v, func(b *testing.B) {
					h := matrixServer(b, v, c)
					matrixVerify(b, h, c)
					template := httptest.NewRequest(c.method, c.target, nil)
					template.Header.Set("Content-Type", c.ct)
					writer := &matrixWriter{header: make(http.Header)}
					b.ReportAllocs()
					b.SetBytes(int64(len(c.body)))
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						// 拷贝只读模板，避免解析缓存跨请求复用。
						// Copy the read-only template to avoid reusing parsed request caches.
						req := *template
						req.Body = io.NopCloser(bytes.NewReader(c.body))
						req.ContentLength = int64(len(c.body))
						clear(writer.header)
						writer.status = 0
						writer.bytes = 0
						h.ServeHTTP(writer, &req)
						if writer.status != 200 || writer.bytes == 0 {
							b.Fatalf("status %d bytes %d", writer.status, writer.bytes)
						}
					}
				})
			}
		})
	}
}

func TestFacadeMatrixQueryEdges(t *testing.T) {
	for _, query := range []string{
		"a=first&a=second&b=&c=hello+world&page=17&tag=one&tag=two",
		"a=%E4%B8%AD%E6%96%87&b=%2B%26%3D&c=%25&page=17&tag=hello+world",
		"a=%&a=valid&b=a;b&c=ok&page=17&tag=first&tag=%&tag=last",
		strings.Repeat("unused=abc&", 128) + "a=last&b=value&c=done&page=17&tag=x",
	} {
		values, _ := url.ParseQuery(query)
		want := matrixParams{Org: "中文", Team: "hello world", User: "user", Repo: "repo", File: "file", A: values.Get("a"), B: values.Get("b"), C: values.Get("c"), Page: 17, Tags: values["tag"]}
		c := matrixCase{name: "edges", method: "GET", target: "/orgs/" + url.PathEscape(want.Org) + "/teams/" + url.PathEscape(want.Team) + "/users/user/repos/repo/files/file?" + query, kind: "params", want: matrixDigest(want)}
		for _, version := range matrixVersions {
			t.Run(version+"/"+fmt.Sprint(len(query)), func(t *testing.T) { matrixVerify(t, matrixServer(t, version, c), c) })
		}
	}
	for _, version := range matrixVersions {
		t.Run(version+"/invalidPage", func(t *testing.T) {
			c := matrixCase{kind: "params", method: "GET"}
			h := matrixServer(t, version, c)
			req := httptest.NewRequest("GET", "/orgs/o/teams/t/users/u/repos/r/files/f?page=invalid", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != 400 {
				t.Fatalf("invalid page status %d", rec.Code)
			}
		})
	}
}

func TestFacadeMatrixSizes(t *testing.T) {
	for _, c := range matrixCases(t) {
		u, err := url.Parse(c.target)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s path=%d query=%d body=%d", c.name, len(u.EscapedPath()), len(u.RawQuery), len(c.body))
	}
}
